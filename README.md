# kcm (Kafka cluster manager)

This is a tool to create, launch and remove Kafka clusters on the local host.

The goal is to make it easy to run Kafka clusters for development and testing.

## Platforms supported

Only Linux and macOS are supported and tested against. It may compile correctly on other platforms but it's possible some things
will not work.

## Installation

Head over to the [releases page](https://github.com/vrischmann/kcm/releases).

Binaries are built for Linux and macOS.

## Building

### Prerequisites

* [Go 1.13](https://golang.org/)
* A C compiler toolchain (`build-essential` on Debian/Ubuntu, `@development-tools` on Fedora, `Xcode` on macOS, etc)

### Procedure

You can simply run `go build` to have a ready to use binary, or use the `build.fish` script so `kcm version` works correctly.

## Usage

First, run `kcm` to see what commands and flags are available:

```
$ kcm
USAGE
  kcm <subcommand> [flag] [args...]

SUBCOMMANDS
  create   create a Kafka cluster with a unique name using the specified version
  list     list the existing Kafka clusters
  status   print the status of the current kafka cluster, if any
  start    start a cluster
  stop     stop a cluster (or all)
  logs     print the logs for a cluster (or all)
  version  print the version information (necessary to report bugs)

FLAGS
  -java-home ...           Use this Java distribution instead of the default one
  -zk-addr 127.0.0.1:2181  The address used by the Zookeeper node
```

**Important note** all flags must come before any positional arguments in these commands.

### Note about Zookeeper

`kcm` manages a single Zookeeper node used by all clusters. To avoid configuration conflicts, each cluster is configured with a Zookeeper prefix (so for example a cluster named `prod` will use the prefix `prod`).

This is important to remember if you interact with the Zookeeper node (for example to create topics).

### Creating a cluster

To create a cluster you must provide a name and the Kafka version to use:

```
$ kcm create staging 1.1.1
Cluster #1 "staging"
           Version           1.1.1
  Broker 1 address  127.0.0.1:9092
  Broker 2 address  127.0.0.1:9093
  Broker 3 address  127.0.0.1:9094
```

By default `create` adds 3 brokers to a cluster. `kcm` choses the port by simply starting from *9092* and incrementing by one for each broker.

You can change the number of brokers to create:

```
$ kcm create -brokers 5 prod 2.3.0
Cluster #2 "prod"
           Version           2.3.0
  Broker 1 address  127.0.0.1:9092
  Broker 2 address  127.0.0.1:9093
  Broker 3 address  127.0.0.1:9094
  Broker 4 address  127.0.0.1:9095
  Broker 5 address  127.0.0.1:9096
```

You can also provide the address for each broker to create:

```
$ kcm create -broker-addr 127.0.0.1:9092 -broker-addr 127.0.0.2:9092 -broker-addr 127.0.0.3:9092 oldprod 0.11.0.3
Cluster #3 "oldprod"
           Version        0.11.0.3
  Broker 1 address  127.0.0.1:9092
  Broker 2 address  127.0.0.2:9092
  Broker 3 address  127.0.0.3:9092
```

The name you chose must be unique. All Kafka versions available from the [Kafka website](http://kafka.apache.org/) should work but I haven't tested everything.

### List

List all existing clusters and their brokers:

```
$ kcm list
Cluster #1 "staging"
           Version           1.1.1
  Broker 1 address  127.0.0.1:9092
  Broker 2 address  127.0.0.1:9093
  Broker 3 address  127.0.0.1:9094


Cluster #2 "prod"
           Version           2.3.0
  Broker 1 address  127.0.0.1:9092
  Broker 2 address  127.0.0.1:9093
  Broker 3 address  127.0.0.1:9094
  Broker 4 address  127.0.0.1:9095
  Broker 5 address  127.0.0.1:9096


Cluster #3 "oldprod"
           Version        0.11.0.3
  Broker 1 address  127.0.0.1:9092
  Broker 2 address  127.0.0.2:9092
  Broker 3 address  127.0.0.3:9092
```

### Status

Print the status of one or all cluster. It also prints the status of the Zookeeper node.

```
$ kcm status
Zookeeper
  version        3.5.5
   status  not started

Cluster #1 "oldprod"
  Broker 1 not started
  Broker 2 not started
  Broker 3 not started
```

Here we see neither Zookeeper is started or the `oldprod` cluster. If we run `kcm start oldprod` we will we see something like this instead:

```
$ kcm status
Zookeeper
  version      3.5.5
   status  pid:15258

Cluster #1 "oldprod"
  Broker 1 started  pid:15304
  Broker 2 started  pid:15305
  Broker 3 started  pid:15313
```

### Start

Starts a cluster. You _can_ have multiple clusters started at the same time as long as you configure the broker addresses correctly to avoid conflicts.

Here's how to start the cluster `oldprod`:

```
$ kcm start oldprod
```

### Stop

Stops a cluster if a name is provided or all of them.

Stopping the cluster `oldprod`:

```
$ kcm stop oldprod
stopping cluster "oldprod"
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
waiting 1s for broker 2 to terminate
waiting 1s for broker 2 to terminate
waiting 1s for broker 2 to terminate
waiting 1s for broker 3 to terminate
waiting 1s for broker 3 to terminate
waiting 1s for broker 3 to terminate
stopped cluster "oldprod"
```

Stopping all clusters:

```
$ kcm stop
stopping cluster "foo"
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
stopped cluster "foo"
stopping cluster "bar"
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
waiting 1s for broker 1 to terminate
stopped cluster "bar"
```

By default the Zookeeper node is not stopped, you can stop it with this command:

```
$ kcm stop --zk
```

### Logs

Tail the logs for a cluster if a name is provided or all them.

Tail the logs for the cluster `prod`:

```
$ kcm logs prod
2019-10-14 00:20:58,766 - INFO  [main:Log4jControllerRegistration$@31] - Registered kafka:type=kafka.Log4jController MBean
2019-10-14 00:20:59,099 - INFO  [main:LoggingSignalHandler@72] - Registered signal handlers for TERM, INT, HUP
2019-10-14 00:20:59,100 - INFO  [main:Logging@66] - starting
2019-10-14 00:20:59,101 - INFO  [main:Logging@66] - Connecting to zookeeper on 127.0.0.1:2181/prod
...
```

Follow the logs for the cluster `prod`:

```
$ kcm logs --follow prod
...
2019-10-14 00:21:00,377 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting preferred replica leader election for partitions
2019-10-14 00:21:00,388 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting the controller scheduler
2019-10-14 00:21:05,390 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Processing automatic preferred replica leader election
```

Follow the logs for all clusters:

```
$ kcm logs --follow
==> /home/vincent/.kcm/prod/broker1/kafka.log <==
...
2019-10-14 00:21:00,377 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting preferred replica leader election for partitions
2019-10-14 00:21:00,388 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting the controller scheduler
2019-10-14 00:21:05,390 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Processing automatic preferred replica leader election

==> /home/vincent/.kcm/staging/broker1/kafka.log <==
...
2019-10-14 00:25:35,346 - INFO  [main:Logging@66] - [KafkaServer id=1] started
2019-10-14 00:25:35,360 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting the controller scheduler
2019-10-14 00:25:40,361 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Processing automatic preferred replica leader election
```

Finally, you can also add the Zookeeper logs:

```
$ kcm logs --follow --zk
==> /home/vincent/.kcm/prod/broker1/kafka.log <==
...
2019-10-14 00:21:00,388 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting the controller scheduler
2019-10-14 00:21:05,390 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Processing automatic preferred replica leader election
2019-10-14 00:26:05,394 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Processing automatic preferred replica leader election

==> /home/vincent/.kcm/staging/broker1/kafka.log <==
...
2019-10-14 00:25:35,346 - INFO  [main:Logging@66] - [KafkaServer id=1] started
2019-10-14 00:25:35,360 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Starting the controller scheduler
2019-10-14 00:25:40,361 - INFO  [controller-event-thread:Logging@66] - [Controller id=1] Processing automatic preferred replica leader election

==> /home/vincent/.kcm/zookeeper.log <==
...
2019-10-14 00:20:58,599 [myid:] - INFO  [main:FileTxnSnapLog@372] - Snapshotting: 0x0 to /home/vincent/.kcm/zkdata/version-2/snapshot.0
2019-10-14 00:20:58,610 [myid:] - INFO  [main:ContainerManager@64] - Using checkIntervalMs=60000 maxPerMinute=10000
2019-10-14 00:20:59,143 [myid:] - INFO  [SyncThread:0:FileTxnLog@216] - Creating new log file: log.1
```

## TODO

* `remove` command
* `complete` command and completion scripts for fish (and maybe bash/zsh if I care to do it)
* ??
