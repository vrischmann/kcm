package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"text/template"
)

const zookeeperVersion = "3.5.5"

func makeZookeeperExtractedPath(version string) string {
	return filepath.Join(dataDir, "zookeeper_"+version)
}
func makeZookeeperTarballPath(version string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("zookeeper-%s.tar.gz", version))
}

// downloadZookeeperArchive downloads a Zookeeper tarball if it doesn't exist.
func downloadZookeeperArchive() error {
	// 1. check if it's already downloaded.
	// If it is we don't have to do anything.

	p := makeZookeeperTarballPath(zookeeperVersion)

	fi, err := os.Stat(p)
	switch {
	case err != nil && !os.IsNotExist(err):
		return err
	case err == nil && fi.IsDir():
		return fmt.Errorf("path %q is a directory, can't extract it", p)
	case err == nil:
		return nil
	}

	// 2. doesn't exist, download the tarball

	filename := fmt.Sprintf("/zookeeper/zookeeper-%s/apache-zookeeper-%s-bin.tar.gz", zookeeperVersion, zookeeperVersion)

	return downloadFromApacheCloser(p, filename)
}

// extractZookeeperArchive extracts the Zookeeper tarball to the kcm data directory.
// This assumes the tarball exists.
func extractZookeeperArchive() error {
	// 1. check if it's already extracted.
	// If it is we don't have to do anything.

	p := makeZookeeperExtractedPath(zookeeperVersion)

	fi, err := os.Stat(p)
	switch {
	case err != nil && !os.IsNotExist(err):
		return err
	case err == nil && !fi.IsDir():
		return fmt.Errorf("path %q is not a directory, can't extract archive %s", p, zookeeperVersion)
	case err == nil:
		return nil
	}

	// 2. doesn't exist, extract the tarball

	return extractTarball(p, makeZookeeperTarballPath(zookeeperVersion))
}

func writeZookeeperLog4jConfig() error {
	const tpl = `zookeeper.root.logger=INFO, F
log4j.rootLogger=${zookeeper.root.logger}
log4j.appender.F=org.apache.log4j.FileAppender
log4j.appender.F.file={{ .LogFile }}
log4j.appender.F.layout=org.apache.log4j.PatternLayout
log4j.appender.F.layout.ConversionPattern=%d{ISO8601} [myid:%X{myid}] - %-5p [%t:%C{1}@%L] - %m%n`

	tmpl, err := template.New("root").Parse(tpl)
	if err != nil {
		return err
	}

	//

	path := makeZookeeperExtractedPath(zookeeperVersion)

	p := filepath.Join(path, "conf/log4j.properties")
	f, err := os.Create(p)
	if err != nil {
		return err
	}

	//

	data := struct {
		LogFile string
	}{
		LogFile: filepath.Join(dataDir, "zookeeper.log"),
	}

	return tmpl.Execute(f, data)
}

func writeZookeeperConfig() error {
	const tpl = `clientPort={{ .Port }}
clientPortAddress={{ .Host }}
dataDir={{ .DataDir }}`

	tmpl, err := template.New("root").Parse(tpl)
	if err != nil {
		return err
	}

	//

	path := makeZookeeperExtractedPath(zookeeperVersion)

	p := filepath.Join(path, "conf/zoo.cfg")
	f, err := os.Create(p)
	if err != nil {
		return err
	}

	//

	host, port, err := net.SplitHostPort(*globalZkAddr)
	if err != nil {
		return err
	}

	data := struct {
		Port    string
		Host    string
		DataDir string
	}{
		Port:    port,
		Host:    host,
		DataDir: filepath.Join(dataDir, "zkdata"),
	}

	return tmpl.Execute(f, data)
}

func startZookeeper(ctx context.Context) error {
	// 1. check if zookeeper is already started

	status, err := getZookeeperStatus(ctx)
	if err != nil {
		return fmt.Errorf("unable to get zookeeper pid. err: %w", err)
	}
	if status.IsStarted() {
		return nil
	}

	// 2. no pid or it isn't started, update the zookeeper status.

	if err := removeZookeeperStatus(ctx); err != nil {
		return fmt.Errorf("unable to update zookeeper status. err: %w", err)
	}

	// 3. download and extract zookeeper if necessary

	if err := downloadZookeeperArchive(); err != nil {
		return fmt.Errorf("unable to download zookeeper archive. err: %w", err)
	}
	if err := extractZookeeperArchive(); err != nil {
		return fmt.Errorf("unable to extract zookeeper archive. err: %w", err)
	}

	// 4. write the zookeeper configuration files

	if err := writeZookeeperConfig(); err != nil {
		return fmt.Errorf("unable to write zookeeper config. err: %w", err)
	}
	if err := writeZookeeperLog4jConfig(); err != nil {
		return fmt.Errorf("unable to write zookeeper log4j config. err: %w", err)
	}

	// 5. prepare the command line to run zookeeper.
	// NOTE(vincent): we don't use the provided shell script, instead we build the proper command line ourselves.

	extractedPath := makeZookeeperExtractedPath(zookeeperVersion)

	cp, err := constructClasspath(filepath.Join(extractedPath, "lib"))
	if err != nil {
		return err
	}

	// 6. finally run the command. This doesn't block.

	bg, err := runBackgroundCommand(ctx, extractedPath,
		getJavaBinary(), "-cp", cp,
		fmt.Sprintf("-Dlog4j.configuration=file://%s/conf/log4j.properties", extractedPath),
		"org.apache.zookeeper.server.quorum.QuorumPeerMain",
		filepath.Join(extractedPath, "conf/zoo.cfg"),
	)
	if err != nil {
		return err
	}

	// 7. update the zookeeper status

	status = zookeeperStatus{
		pid: bg.pid,
	}

	if err := setZookeeperStatus(ctx, status); err != nil {
		return err
	}

	return nil
}

func stopZookeeper(ctx context.Context) error {
	// 1. check if zookeeper is started
	status, err := getZookeeperStatus(ctx)
	if err != nil {
		return fmt.Errorf("unable to get zookeeper pid. err: %w", err)
	}
	if !status.IsValid() {
		return nil
	}
	if !status.IsStarted() {
		return nil
	}

	// 2. terminate zookeeper
	if err := syscall.Kill(status.pid, syscall.Signal(15)); err != nil {
		return fmt.Errorf("unable to send interrupt signal to zookeeper. err: %w", err)
	}

	// 3. remove its status
	if err := removeZookeeperStatus(ctx); err != nil {
		return err
	}

	return nil
}
