package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"text/tabwriter"
)

type ClusterName string
type KafkaVersion string

type Broker struct {
	ID   int
	Addr net.TCPAddr
}

type Cluster struct {
	ID      int
	Name    ClusterName
	Version KafkaVersion

	Brokers []Broker
}

func (c Cluster) String() string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "Cluster #%d %q\n", c.ID, c.Name)

	w := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', tabwriter.AlignRight)

	fmt.Fprintf(w, "Version\t%s\t\n", c.Version)
	for _, broker := range c.Brokers {
		fmt.Fprintf(w, "Broker %d address\t%s\t\n", broker.ID, broker.Addr.String())
	}

	w.Flush()

	return builder.String()
}

//go:generate stringer -type=ClusterState -linecomment
// type ClusterState int

// const (
// 	ClusterStopped ClusterState = iota // stopped
// 	ClusterStarted                     // started
// )

type zookeeperStatus struct {
	pid int
}

func (s zookeeperStatus) IsValid() bool {
	return s.pid > 0
}

func (s zookeeperStatus) IsStarted() bool {
	return s.IsValid() && pidExists(s.pid)
}

type brokerStatus struct {
	pid     int
	cluster Cluster
	broker  Broker
}

func (s brokerStatus) IsValid() bool {
	return s.pid > 0
}

func (s brokerStatus) IsStarted() bool {
	return s.IsValid() && pidExists(s.pid)
}

type clusterStatus struct {
	brokers []brokerStatus
}

func (c clusterStatus) String() string {
	var builder strings.Builder

	w := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', tabwriter.AlignRight)

	for _, s := range c.brokers {
		if s.IsStarted() {
			fmt.Fprintf(w, "Broker %d started\tpid:%d\t\n", s.broker.ID, s.pid)
		} else {
			fmt.Fprintf(w, "Broker %d not started\t\t\n", s.broker.ID)
		}
	}

	w.Flush()

	return builder.String()
}

func getClusterStatus(ctx context.Context, cluster Cluster) (clusterStatus, error) {
	var status clusterStatus
	for _, broker := range cluster.Brokers {
		tmp, err := getBrokerStatus(ctx, cluster, broker)
		if err != nil {
			return clusterStatus{}, err
		}

		status.brokers = append(status.brokers, tmp)
	}
	return status, nil
}
