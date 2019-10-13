package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// initDirectories initializes the user directories necessary
// to store data.
func initDirectories() error {
	// cacheDir is used to store the Kafka and Zookeeper archives
	dir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cacheDir = filepath.Join(dir, "kcm")

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	// dataDir is used to store the database and other config files
	dir, err = os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	dataDir = filepath.Join(dir, ".kcm")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}

	return nil
}

type brokerListenAddrs []net.TCPAddr

func (s *brokerListenAddrs) Set(tmp string) error {
	addr, err := net.ResolveTCPAddr("tcp", tmp)
	if err != nil {
		return err
	}

	*s = append(*s, *addr)

	return nil
}

func (s *brokerListenAddrs) String() string {
	var builder strings.Builder
	for i, addr := range *s {
		if i+1 < len(*s) {
			builder.WriteString(", ")
		}
		builder.WriteString(addr.String())
	}
	return builder.String()
}

var _ flag.Value = (*brokerListenAddrs)(nil)

func mustResolveTCPAddr(s string) net.TCPAddr {
	addr, err := net.ResolveTCPAddr("tcp", s)
	if err != nil {
		log.Fatal(err)
	}
	return *addr
}

func downloadFromApacheArchive(dst, filename string) error {
	const baseURL = "https://archive.apache.org/dist/"

	u := baseURL + filename

	log.Printf("downloading %s", u)

	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tarball, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(tarball, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

var errNotFound = errors.New("not found")

func downloadFromApacheCloser(dst, filename string) error {
	const baseURL = "https://www.apache.org/dyn/closer.cgi"

	params := make(url.Values)
	params.Set("filename", filename)
	params.Set("action", "download")

	log.Printf("downloading %s", baseURL+"?"+params.Encode())

	resp, err := http.Get(baseURL + "?" + params.Encode())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}

	tarball, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(tarball, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

// extractTarball extracts a tar.gz tarball into the dst directory.
// It always strips the first level.
func extractTarball(dst string, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	gzf, err := gzip.NewReader(f)
	if err != nil {
		return err
	}

	tr := tar.NewReader(gzf)

	stripFirstLevel := func(s string) string {
		return s[strings.IndexRune(s, '/')+1:]
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			// strip the first level which is always kafka_2.12_<version> and not needed
			relativePath := stripFirstLevel(hdr.Name)
			p := filepath.Join(dst, relativePath)

			if err := os.MkdirAll(p, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("unable to create directory %q. err: %w", p, err)
			}

		case tar.TypeReg:
			// strip the first level which is always kafka_2.12_<version> and not needed
			relativePath := stripFirstLevel(hdr.Name)
			p := filepath.Join(dst, relativePath)

			dir := filepath.Dir(p)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("unable to create directory %q. err: %w", p, err)
			}

			f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("unable to create file %q. err: %w", p, err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				return fmt.Errorf("unable to copy file data. err: %w", err)
			}

			if err := f.Close(); err != nil {
				return fmt.Errorf("unable to close file. err: %w", err)
			}
		}
	}
}

type backgroundCommand struct {
	pid int
}

func runBackgroundCommand(ctx context.Context, dir string, command string, args ...string) (*backgroundCommand, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = ioutil.Discard
	cmd.Stderr = ioutil.Discard

	// log.Printf("env: %v", cmd.Env)
	// log.Printf("running %s %v in %s", command, args, dir)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &backgroundCommand{
		pid: cmd.Process.Pid,
	}, nil
}

func constructClasspath(path string) (string, error) {
	var cp []string
	err := filepath.Walk(path, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(fi.Name(), ".jar") {
			cp = append(cp, path)
		}

		return nil
	})
	return strings.Join(cp, ":"), err
}

func pidExists(pid int) bool {
	err := syscall.Kill(pid, syscall.Signal(0))
	switch {
	case err == syscall.EPERM:
		// The process should always be owned by the current user so we can manipulate it.
		// If it's not something is wrong so we crash.
		log.Fatalf("process %d is not owned by the current user, this should not happen", pid)

	case err == syscall.ESRCH:
		return false

	case err == nil:
		return true

	default:
		var errno syscall.Errno
		if errors.As(err, &errno) {
			log.Printf("errno: %d", errno)
		}

		log.Fatalf("couldn't send signal to process %d. err: %v", pid, err)
	}

	// unreachable
	return false
}

// tailFiles tails a file, optionally following changes.
func tailFiles(follow bool, files ...string) error {
	// NOTE(vincent): not worth it reimplementing tail,
	// delegate to the `tail` binary.

	args := files[:0]

	for _, file := range files {
		_, err := os.Stat(file)
		switch {
		case os.IsNotExist(err):
			continue
		case err != nil:
			return err
		default:
			args = append(args, file)
		}
	}

	if follow {
		args = append([]string{"-f"}, args...)
	}

	cmd := exec.Command("tail", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
