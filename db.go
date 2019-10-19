package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"crawshaw.io/sqlite"
	"crawshaw.io/sqlite/sqlitex"
)

func getZookeeperStatus(ctx context.Context) (zookeeperStatus, error) {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	stmt := conn.Prep(`SELECT process_id FROM zookeeper_status LIMIT 1`)

	var status zookeeperStatus
	for {
		if hasNext, err := stmt.Step(); err != nil {
			return status, err
		} else if !hasNext {
			break
		}

		status.pid = int(stmt.GetInt64("process_id"))
	}

	return status, nil
}

func setZookeeperStatus(ctx context.Context, status zookeeperStatus) error {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	stmt := conn.Prep(`INSERT INTO zookeeper_status(process_id) VALUES ($process_id)`)
	stmt.SetInt64("$process_id", int64(status.pid))

	_, err := stmt.Step()
	return err
}

func removeZookeeperStatus(ctx context.Context) error {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	stmt := conn.Prep(`DELETE FROM zookeeper_status`)

	_, err := stmt.Step()
	return err
}

func getBrokerStatus(ctx context.Context, cluster Cluster, broker Broker) (brokerStatus, error) {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	var status brokerStatus
	status.cluster = cluster
	status.broker = broker

	stmt := conn.Prep(`SELECT process_id FROM broker_status
				WHERE cluster_id = $cluster_id
				AND broker_id = $broker_id`)
	stmt.SetInt64("$cluster_id", int64(cluster.ID))
	stmt.SetInt64("$broker_id", int64(broker.ID))

	for {
		if hasNext, err := stmt.Step(); err != nil {
			return status, err
		} else if !hasNext {
			break
		}

		status.pid = int(stmt.GetInt64("process_id"))
	}

	return status, nil
}

func setBrokerStatus(ctx context.Context, status brokerStatus) error {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	stmt := conn.Prep(`INSERT INTO broker_status(process_id, cluster_id, broker_id) VALUES ($process_id, $cluster_id, $broker_id)`)
	stmt.SetInt64("$process_id", int64(status.pid))
	stmt.SetInt64("$cluster_id", int64(status.cluster.ID))
	stmt.SetInt64("$broker_id", int64(status.broker.ID))

	_, err := stmt.Step()
	return err
}

func removeBrokerStatus(ctx context.Context, cluster Cluster, broker Broker) error {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	stmt := conn.Prep(`DELETE FROM broker_status
				WHERE cluster_id = $cluster_id
				AND broker_id = $broker_id`)
	stmt.SetInt64("$cluster_id", int64(cluster.ID))
	stmt.SetInt64("$broker_id", int64(broker.ID))

	_, err := stmt.Step()
	return err
}

func createCluster(ctx context.Context, cluster Cluster) (err error) {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	defer sqlitex.Save(conn)(&err)

	// Create cluster row
	stmt := conn.Prep(`INSERT INTO cluster(name, version) VALUES($name, $version)`)
	stmt.SetText("$name", string(cluster.Name))
	stmt.SetText("$version", string(cluster.Version))

	if _, err := stmt.Step(); err != nil {
		return err
	}

	id := conn.LastInsertRowID()

	// Create brokers

	for _, broker := range cluster.Brokers {
		stmt := conn.Prep(`INSERT INTO broker(id, cluster_id, addr) VALUES($id, $cluster_id, $addr)`)

		stmt.SetInt64("$id", int64(broker.ID))
		stmt.SetInt64("$cluster_id", id)
		stmt.SetText("$addr", broker.Addr.String())

		if _, err := stmt.Step(); err != nil {
			return err
		}
	}

	return
}

func getFirstClusterFromStmt(stmt *sqlite.Stmt) (*Cluster, error) {
	clusters, err := getClustersFromStmt(stmt)
	if err != nil {
		return nil, err
	}
	if len(clusters) == 0 {
		return nil, nil
	}
	return &clusters[0], nil
}

func getCluster(ctx context.Context, name ClusterName) (*Cluster, error) {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	stmt := conn.Prep(`SELECT b.id AS broker_id, b.addr, c.name, c.id AS cluster_id, c.version
				FROM cluster c INNER JOIN broker b ON b.cluster_id = c.id
				WHERE c.name = $name`)
	stmt.SetText("$name", string(name))

	return getFirstClusterFromStmt(stmt)
}

func searchClusters(ctx context.Context, pattern string) ([]Cluster, error) {
	conn := pool.Get(ctx)
	defer pool.Put(conn)

	const q = `SELECT b.id AS broker_id, b.addr, c.name, c.id AS cluster_id, c.version
			FROM cluster c INNER JOIN broker b ON b.cluster_id = c.id`

	var stmt *sqlite.Stmt
	switch {
	case pattern != "":
		stmt = conn.Prep(q + ` WHERE c.name LIKE $pattern`)
		stmt.SetText("$pattern", "%"+pattern+"%")

	default:
		stmt = conn.Prep(q)
	}

	return getClustersFromStmt(stmt)
}

func getClustersFromStmt(stmt *sqlite.Stmt) ([]Cluster, error) {
	defer stmt.Reset()

	var (
		clusters []Cluster

		current *Cluster
	)

	for {
		if hasNext, err := stmt.Step(); err != nil {
			return nil, err
		} else if !hasNext {
			break
		}

		//

		id := int(stmt.GetInt64("cluster_id"))

		switch {
		case current == nil:
			current = new(Cluster)
		case id != current.ID:
			clusters = append(clusters, *current)
			current = new(Cluster)
		}

		current.ID = id
		current.Name = ClusterName(stmt.GetText("name"))
		current.Version = KafkaVersion(stmt.GetText("version"))
		current.Brokers = append(current.Brokers, Broker{
			ID:   int(stmt.GetInt64("broker_id")),
			Addr: mustResolveTCPAddr(stmt.GetText("addr")),
		})
	}

	if current != nil {
		clusters = append(clusters, *current)
	}

	return clusters, nil
}

func cleanupDatabase() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 1. clean up the clusters

	clusters, err := searchClusters(ctx, "")
	if err != nil {
		return err
	}

	for _, cluster := range clusters {
		status, err := getClusterStatus(ctx, cluster)
		if err != nil {
			return err
		}

		for _, s := range status.brokers {
			if !s.IsStarted() {
				if err := removeBrokerStatus(ctx, cluster, s.broker); err != nil {
					return err
				}
			}
		}
	}

	// 2. clean up zookeeper

	status, err := getZookeeperStatus(ctx)
	if err != nil {
		return err
	}

	if !status.IsStarted() {
		if err := removeZookeeperStatus(ctx); err != nil {
			return err
		}
	}

	return nil
}

func initializeDatabase(pool *sqlitex.Pool) error {
	conn := pool.Get(nil)
	defer pool.Put(conn)

	if err := sqlitex.ExecTransient(conn, "PRAGMA journal_mode=WAL;", nil); err != nil {
		return err
	}
	if err := sqlitex.ExecTransient(conn, "PRAGMA foreign_keys=ON;", nil); err != nil {
		return err
	}
	if err := sqlitex.ExecScript(conn, schema); err != nil {
		return err
	}
	return nil
}

func openDatabase() error {
	database := filepath.Join(dataDir, "database.db")
	dsn := fmt.Sprintf("file:%s", database)

	var err error
	pool, err = sqlitex.Open(dsn, 0, 4)
	if err != nil {
		return err
	}

	if err := initializeDatabase(pool); err != nil {
		return err
	}

	return nil
}

const schema = `
PRAGMA auto_vacuum = FULL;

CREATE TABLE IF NOT EXISTS cluster (
	id integer NOT NULL,
	name text NOT NULL,
	version text NOT NULL,
	PRIMARY KEY (id)
);
CREATE UNIQUE INDEX IF NOT EXISTS cluster_name ON cluster(name);

CREATE TABLE IF NOT EXISTS broker (
	id integer NOT NULL,
	cluster_id integer NOT NULL,
	addr text NOT NULL,
	PRIMARY KEY (id, cluster_id),
	FOREIGN KEY (cluster_id) REFERENCES cluster(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS broker_status (
	process_id integer NOT NULL,
	cluster_id integer NOT NULL,
	broker_id integer NOT NULL,
	PRIMARY KEY (process_id),
	FOREIGN KEY (broker_id, cluster_id) REFERENCES broker(id, cluster_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS current_context (
	cluster_id integer NOT NULL,
	state integer NOT NULL
);

CREATE TABLE IF NOT EXISTS zookeeper_status (
	process_id integer NOT NULL,
	PRIMARY KEY (process_id)
);
`
