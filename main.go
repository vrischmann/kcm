package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
	"github.com/peterbourgon/ff/ffcli"
)

var (
	gVersion string
	gCommit  string

	dataDir  string
	cacheDir string

	pool *sqlitex.Pool

	//

	globalFlags    = flag.NewFlagSet("kcm", flag.ExitOnError)
	globalJavaHome = globalFlags.String("java-home", "", "Use this Java distribution instead of the default one")
	globalZkAddr   = globalFlags.String("zk-addr", "127.0.0.1:2181", "The address used by the Zookeeper node")

	createFlags       = flag.NewFlagSet("create", flag.ExitOnError)
	createBrokers     = createFlags.Int("brokers", 3, "the number of brokers to add to the cluster")
	createBrokerAddrs brokerListenAddrs

	stopFlags = flag.NewFlagSet("stop", flag.ExitOnError)
	stopZk    = stopFlags.Bool("zk", false, "Stop Zookeeper too")

	logsFlags  = flag.NewFlagSet("logs", flag.ExitOnError)
	logsZk     = logsFlags.Bool("zk", false, "Print the Zookeeper logs too")
	logsFollow = logsFlags.Bool("follow", false, "Follow the logs as changes are made")
)

func init() {
	createFlags.Var(&createBrokerAddrs, "broker-addr", "the address of a broker (can be provided multiple times)")
}

func printCluster(cluster *Cluster) {
	if cluster == nil {
		log.Printf("no cluster")
		return
	}

	log.Println(cluster.String())
}

func runCreateCluster(name ClusterName, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	//

	tmp := Cluster{Name: ClusterName(name), Version: KafkaVersion(version)}

	switch {
	case len(createBrokerAddrs) > 0:
		for i, addr := range createBrokerAddrs {
			tmp.Brokers = append(tmp.Brokers, Broker{
				ID:   i + 1,
				Addr: addr,
			})
		}

	default:
		for i := 0; i < *createBrokers; i++ {
			addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", 9092+i))
			if err != nil {
				return err
			}

			tmp.Brokers = append(tmp.Brokers, Broker{
				ID:   i + 1,
				Addr: *addr,
			})
		}
	}

	if err := createCluster(ctx, tmp); err != nil {
		if sqlite.ErrCode(err) == sqlite.SQLITE_CONSTRAINT_UNIQUE {
			log.Printf("cluster named %q already exists", name)
			return nil
		}
		return err
	}

	//

	cluster, err := getCluster(ctx, name)
	if err != nil {
		return err
	}

	printCluster(cluster)

	return nil
}

func runListClusters(pattern string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	//

	clusters, err := searchClusters(ctx, pattern)
	if err != nil {
		return err
	}

	for i, cluster := range clusters {
		printCluster(&cluster)
		if i+1 < len(clusters) {
			log.Println("")
		}
	}

	return nil
}

func runStatus(name ClusterName) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// print zookeeper's status.

	status, err := getZookeeperStatus(ctx)
	if err != nil {
		return err
	}

	log.Printf("Zookeeper")

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintf(w, "version\t%s\t\n", zookeeperVersion)
	if status.IsStarted() {
		fmt.Fprintf(w, "status\tpid:%d\t\n", status.pid)
	} else {
		fmt.Fprintf(w, "status\tnot started\t\n")
	}
	w.Flush()

	log.Println()

	//

	switch {
	case name != "":
		cluster, err := getCluster(ctx, name)
		if err != nil {
			return err
		}
		if cluster == nil {
			return fmt.Errorf("cluster %q doesn't exist", name)
		}

		status, err := getClusterStatus(ctx, *cluster)
		if err != nil {
			return err
		}
		log.Printf("%v", status)

	default:
		clusters, err := searchClusters(ctx, "")
		if err != nil {
			return err
		}

		for _, cluster := range clusters {
			log.Printf("Cluster #%d %q", cluster.ID, cluster.Name)

			status, err := getClusterStatus(ctx, cluster)
			if err != nil {
				return err
			}
			log.Printf("%v\n", status)
		}
	}

	return nil
}

func runStart(name ClusterName) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	//

	cluster, err := getCluster(ctx, name)
	if err != nil {
		return err
	}
	if cluster == nil {
		log.Printf("cluster %q doesn't exist", name)
		return nil
	}

	// Start zookeeper first.

	ctx = context.Background()

	if err := startZookeeper(ctx); err != nil {
		return err
	}
	log.Printf("launched zookeeper")

	// set up a cancelable context to stop the cluster
	//

	if err := startCluster(ctx, *cluster); err != nil {
		return err
	}
	log.Printf("launched cluster %q", cluster.Name)

	return nil
}

func runStop(name ClusterName) error {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	switch {
	case name != "":
		cluster, err := getCluster(ctx, name)
		if err != nil {
			return err
		}
		if cluster == nil {
			log.Printf("cluster %q doesn't exist", name)
			return nil
		}

		log.Printf("stopping cluster %q", cluster.Name)
		if err := stopCluster(ctx, *cluster); err != nil {
			return err
		}
		log.Printf("stopped cluster %q", cluster.Name)

	default:
		clusters, err := searchClusters(ctx, "")
		if err != nil {
			return err
		}

		for _, cluster := range clusters {
			log.Printf("stopping cluster %q", cluster.Name)
			if err := stopCluster(ctx, cluster); err != nil {
				return err
			}
			log.Printf("stopped cluster %q", cluster.Name)
		}
	}

	if *stopZk {
		log.Printf("stopping zookeeper")
		if err := stopZookeeper(ctx); err != nil {
			return err
		}
		log.Printf("stopped zookeeper")
	}

	return nil
}

func runLogs(name ClusterName) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// files contains all files that need to be tailed
	var files []string

	addBrokerLog := func(cluster Cluster, broker Broker) {
		dir := makeBrokerDir(cluster.Name, broker.ID)
		logFile := filepath.Join(dir, "kafka.log")

		files = append(files, logFile)
	}

	switch {
	case name != "":
		cluster, err := getCluster(ctx, name)
		if err != nil {
			return err
		}
		if cluster == nil {
			log.Printf("cluster %q doesn't exist", name)
			return nil
		}

		for _, broker := range cluster.Brokers {
			addBrokerLog(*cluster, broker)
		}

	default:
		clusters, err := searchClusters(ctx, "")
		if err != nil {
			return err
		}

		for _, cluster := range clusters {
			for _, broker := range cluster.Brokers {
				addBrokerLog(cluster, broker)
			}
		}
	}

	if *logsZk {
		files = append(files, filepath.Join(dataDir, "zookeeper.log"))
	}

	return tailFiles(*logsFollow, files...)
}

func main() {
	log.SetFlags(0)

	// Initialize all necessary directories
	if err := initDirectories(); err != nil {
		log.Fatal(err)
	}

	// Open and maybe initialize the database
	if err := openDatabase(); err != nil {
		log.Fatal(err)
	}

	// it's possible the host has rebooted and the database is out of sync
	// so we clean up the database if necessary.

	if err := cleanupDatabase(); err != nil {
		log.Fatal(err)
	}

	//

	createCmd := &ffcli.Command{
		Name:      "create",
		Usage:     "create <name> <version>",
		FlagSet:   createFlags,
		ShortHelp: "create a Kafka cluster with a unique name using the specified version",
		Exec: func(args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("Usage: kcm <name> <version>")
			}
			return runCreateCluster(ClusterName(args[0]), args[1])
		},
	}

	listCmd := &ffcli.Command{
		Name:      "list",
		Usage:     "list [pattern]",
		ShortHelp: "list the existing Kafka clusters",
		LongHelp: `list the existing Kafka clusters.

The list can be filtered using the "pattern" argument which is a regex.`,
		Exec: func(args []string) error {
			if len(args) == 0 {
				return runListClusters("")
			}
			return runListClusters(args[0])
		},
	}

	statusCmd := &ffcli.Command{
		Name:      "status",
		Usage:     "status",
		ShortHelp: "print the status of the current kafka cluster, if any",
		Exec: func(args []string) error {
			if len(args) < 1 {
				return runStatus("")
			}
			return runStatus(ClusterName(args[0]))
		},
	}

	startCmd := &ffcli.Command{
		Name:      "start",
		Usage:     "start <cluster>",
		ShortHelp: "start a cluster",
		Exec: func(args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("Usage: kcm start <cluster>")
			}
			return runStart(ClusterName(args[0]))
		},
	}

	stopCmd := &ffcli.Command{
		Name:      "stop",
		FlagSet:   stopFlags,
		Usage:     "stop [cluster]",
		ShortHelp: "stop a cluster (or all)",
		Exec: func(args []string) error {
			if len(args) < 1 {
				return runStop("")
			}
			return runStop(ClusterName(args[0]))
		},
	}

	logsCmd := &ffcli.Command{
		Name:      "logs",
		FlagSet:   logsFlags,
		Usage:     "logs [cluster]",
		ShortHelp: "print the logs for a cluster (or all)",
		Exec: func(args []string) error {
			if len(args) < 1 {
				return runLogs("")
			}
			return runLogs(ClusterName(args[0]))
		},
	}

	versionCmd := &ffcli.Command{
		Name:      "version",
		Usage:     "version",
		ShortHelp: "print the version information (necessary to report bugs)",
		Exec: func([]string) error {
			version := gVersion
			if version == "" {
				version = "unknown"
			}
			commit := gCommit
			if commit == "" {
				commit = "unknown"
			}
			log.Printf("kcm version %s, commit %s", version, commit)
			return nil
		},
	}

	rootCmd := &ffcli.Command{
		Usage:     "kcm <subcommand> [flag] [args...]",
		FlagSet:   globalFlags,
		ShortHelp: "manage Kafka clusters for local development and testing",
		Subcommands: []*ffcli.Command{
			createCmd, listCmd, statusCmd,
			startCmd, stopCmd, logsCmd,
			versionCmd,
		},
		Exec: func([]string) error {
			return flag.ErrHelp
		},
	}

	if err := rootCmd.Run(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			rootCmd.FlagSet.Usage()
		} else {
			log.Fatal(err)
		}
	}
}
