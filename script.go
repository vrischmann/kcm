package main

import "fmt"

type kafkaScriptConnection int

const (
	kafkaScriptZookeeper kafkaScriptConnection = iota + 1
	kafkaScriptKafka
)

func (c kafkaScriptConnection) DefaultFlagName() string {
	switch c {
	case kafkaScriptZookeeper:
		return "--zookeeper"
	case kafkaScriptKafka:
		return "--bootstrap-server"
	default:
		panic(fmt.Errorf("invalid connection type %v", c))
	}
}

type kafkaScriptRequirement struct {
	connect  kafkaScriptConnection
	flagName string
}

func (r kafkaScriptRequirement) FlagName() string {
	if r.flagName != "" {
		return r.flagName
	}
	return r.connect.DefaultFlagName()
}

var kafkaScriptsRequirements = map[string]kafkaScriptRequirement{
	"kafka-acls.sh":                       {kafkaScriptKafka, ""},
	"kafka-broker-api-versions.sh":        {kafkaScriptKafka, ""},
	"kafka-configs.sh":                    {kafkaScriptKafka, ""},
	"kafka-console-consumer.sh":           {kafkaScriptKafka, ""},
	"kafka-console-producer.sh":           {kafkaScriptKafka, "--broker-list"},
	"kafka-consumer-groups.sh":            {kafkaScriptKafka, ""},
	"kafka-consumer-perf-test.sh":         {kafkaScriptKafka, "--broker-list"},
	"kafka-delegation-tokens.sh":          {kafkaScriptKafka, ""},
	"kafka-delete-records.sh":             {kafkaScriptKafka, ""},
	"kafka-preferred-replica-election.sh": {kafkaScriptZookeeper, ""},
	"kafka-reassign-partitions.sh":        {kafkaScriptKafka, ""},
	"kafka-replica-verification.sh":       {kafkaScriptKafka, "--broker-list"},
	"kafka-streams-application-reset.sh":  {kafkaScriptKafka, "--bootstrap-servers"},
	"kafka-topics.sh":                     {kafkaScriptZookeeper, ""},
	"kafka-verifiable-consumer.sh":        {kafkaScriptKafka, "--broker-list"},
	"kafka-verifiable-producer.sh":        {kafkaScriptKafka, "--broker-list"},
}
