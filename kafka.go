package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"text/template"
	"time"
)

func makeKafkaExtractedPath(version KafkaVersion) string {
	return filepath.Join(dataDir, "kafka_"+string(version))
}

func makeKafkaTarballPath(version KafkaVersion) string {
	return filepath.Join(cacheDir, "kafka_"+string(version)+".tar.gz")
}

// extractKafkaArchive extracts a tarball of Kafka to the kcm data directory.
// This assumes the tarball exists.
func extractKafkaArchive(version KafkaVersion) error {
	// 1. check if it's already extracted.
	// If it is we don't have to do anything.

	extractedPath := makeKafkaExtractedPath(version)

	fi, err := os.Stat(extractedPath)
	switch {
	case err != nil && !os.IsNotExist(err):
		return err
	case err == nil && !fi.IsDir():
		return fmt.Errorf("path %q is not a directory, can't extract Kafka archive %s", extractedPath, version)
	case err == nil:
		return nil
	}

	// 2. doesn't exist, extract the tarball

	return extractTarball(extractedPath, makeKafkaTarballPath(version))
}

// downloadKafkaArchive downloads a Kafka tarball if it doesn't exist.
func downloadKafkaArchive(version KafkaVersion) error {
	// 1. check if it's already downloaded.
	// If it is we don't have to do anything.

	p := makeKafkaTarballPath(version)

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

	filename := fmt.Sprintf("/kafka/%s/kafka_2.12-%s.tgz", version, version)
	err = downloadFromApacheCloser(p, filename)
	if err == nil {
		return nil
	}
	if err != nil && err != errNotFound {
		return err
	}

	filename = fmt.Sprintf("/kafka/%s/kafka_2.12-%s.tgz", version, version)
	return downloadFromApacheArchive(p, filename)
}

func makeBrokerDir(name ClusterName, id int) string {
	return filepath.Join(dataDir, string(name), fmt.Sprintf("broker%d", id))
}

func writeKafkaLog4jConfig(cluster Cluster, broker Broker) error {
	const tpl = `log4j.rootLogger=INFO, F
log4j.appender.F=org.apache.log4j.FileAppender
log4j.appender.F.file={{ .LogFile }}
log4j.appender.F.layout=org.apache.log4j.PatternLayout
log4j.appender.F.layout.ConversionPattern=%d{ISO8601} - %-5p [%t:%C{1}@%L] - %m%n`

	tmpl, err := template.New("root").Parse(tpl)
	if err != nil {
		return err
	}

	//

	brokerPath := makeBrokerDir(cluster.Name, broker.ID)

	p := filepath.Join(brokerPath, "log4j.properties")
	f, err := os.Create(p)
	if err != nil {
		return err
	}

	//

	data := struct {
		LogFile string
	}{
		LogFile: filepath.Join(brokerPath, "kafka.log"),
	}

	return tmpl.Execute(f, data)
}

func writeKafkaConfig(cluster Cluster, broker Broker) error {
	const tpl = `broker.id={{ .BrokerID }}
listeners=PLAINTEXT://{{ .Addr }}
log.dirs={{ .LogDir }}
offsets.topic.replication.factor=1
transaction.state.log.replication.factor=1
transaction.state.log.min.isr=1
log.retention.hours=168
zookeeper.connect={{ .ZkAddr }}/{{ .ZkPrefix }}
zookeeper.connection.timeout.ms=10000
group.initial.rebalance.delay.ms=0
`

	tmpl, err := template.New("root").Parse(tpl)
	if err != nil {
		return err
	}

	//

	path := makeBrokerDir(cluster.Name, broker.ID)
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}

	p := filepath.Join(path, "server.properties")
	f, err := os.Create(p)
	if err != nil {
		return err
	}

	//

	data := struct {
		BrokerID int
		Addr     string
		LogDir   string
		ZkAddr   string
		ZkPrefix string
	}{
		BrokerID: broker.ID,
		Addr:     broker.Addr.String(),
		LogDir:   filepath.Join(path, "data"),
		ZkAddr:   *globalZkAddr,
		ZkPrefix: string(cluster.Name),
	}

	return tmpl.Execute(f, data)
}

func startBroker(ctx context.Context, cluster Cluster, broker Broker) error {
	// 1. check if the cluster is already started.
	status, err := getBrokerStatus(ctx, cluster, broker)
	if err != nil {
		return fmt.Errorf("unable to get kafka broker pid. err: %w", err)
	}
	if status.IsStarted() {
		return nil
	}

	// 2. no pid or it isn't started, update the broker status.

	if err := removeBrokerStatus(ctx, cluster, broker); err != nil {
		return fmt.Errorf("unable to update broker %d status. err: %w", broker.ID, err)
	}

	// 3. download and extract kafka if necessary

	if err := downloadKafkaArchive(cluster.Version); err != nil {
		return fmt.Errorf("unable to download archive. err: %w", err)
	}
	if err := extractKafkaArchive(cluster.Version); err != nil {
		return fmt.Errorf("unable to extract archive. err: %w", err)
	}

	// 4. write the kafka configuration files

	if err := writeKafkaConfig(cluster, broker); err != nil {
		return err
	}
	if err := writeKafkaLog4jConfig(cluster, broker); err != nil {
		return err
	}

	// 5. prepare the command line to run kafka.
	// NOTE(vincent): we don't use the provided shell script, instead we build the proper command line ourselves.

	extractedPath := makeKafkaExtractedPath(cluster.Version)
	config := filepath.Join(makeBrokerDir(cluster.Name, broker.ID), "server.properties")
	log4jConfig := filepath.Join(makeBrokerDir(cluster.Name, broker.ID), "log4j.properties")

	cp, err := constructClasspath(filepath.Join(extractedPath, "libs"))
	if err != nil {
		return err
	}

	var javaBinary string
	if *globalJavaHome != "" {
		javaBinary = *globalJavaHome + "/bin/java"
	} else {
		javaBinary = "java"
	}

	// 6. finally run the command. This doesn't block.

	bg, err := runBackgroundCommand(ctx, extractedPath,
		javaBinary, "-cp", cp,
		"-Dlog4j.configuration=file:"+log4jConfig,
		"kafka.Kafka", config,
	)
	if err != nil {
		return err
	}

	// 7. update the broker status

	newStatus := brokerStatus{
		pid:     bg.pid,
		cluster: cluster,
		broker:  broker,
	}

	if err := setBrokerStatus(ctx, newStatus); err != nil {
		return err
	}

	return nil
}

func stopBroker(ctx context.Context, cluster Cluster, broker Broker) error {
	// 1. check if the broker is started.
	status, err := getBrokerStatus(ctx, cluster, broker)
	if err != nil {
		return fmt.Errorf("unable to get kafka broker pid. err: %w", err)
	}
	if !status.IsValid() || !status.IsStarted() {
		return nil
	}

	// 2. terminate the broker
	if err := syscall.Kill(status.pid, syscall.Signal(15)); err != nil {
		return fmt.Errorf("unable to send interrupt signal to broker %d. err: %w", broker.ID, err)
	}

	// 3. wait for the broker to be terminated
	for status.IsStarted() {
		log.Printf("waiting 1s for broker %d to terminate", broker.ID)
		time.Sleep(1 * time.Second)
	}

	// 4. broker terminated, remove its status
	if err := removeBrokerStatus(ctx, cluster, broker); err != nil {
		return err
	}

	return nil
}

func removeBrokerData(cluster Cluster, broker Broker) error {
	dir := makeBrokerDir(cluster.Name, broker.ID)
	log.Printf("removing data dir %s", dir)
	return os.RemoveAll(dir)
}

func stopCluster(ctx context.Context, cluster Cluster) error {
	for _, broker := range cluster.Brokers {
		if err := stopBroker(ctx, cluster, broker); err != nil {
			return err
		}
	}

	return nil
}

func startCluster(ctx context.Context, cluster Cluster) error {
	for _, broker := range cluster.Brokers {
		if err := startBroker(ctx, cluster, broker); err != nil {
			return err
		}
	}

	return nil
}
