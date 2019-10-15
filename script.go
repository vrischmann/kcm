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
	"kafka-acls":                       {kafkaScriptKafka, ""},
	"kafka-broker-api-versions":        {kafkaScriptKafka, ""},
	"kafka-configs":                    {kafkaScriptKafka, ""},
	"kafka-console-consumers":          {kafkaScriptKafka, ""},
	"kafka-consumer-groups":            {kafkaScriptKafka, ""},
	"kafka-delegation-tokens":          {kafkaScriptKafka, ""},
	"kafka-delete-records":             {kafkaScriptKafka, ""},
	"kafka-log-dirs":                   {kafkaScriptKafka, ""},
	"kafka-preferred-replica-election": {kafkaScriptZookeeper, ""},
	"kafka-reassign-partitions":        {kafkaScriptKafka, ""},
	"kafka-streams-application-reset":  {kafkaScriptKafka, "--bootstrap-servers"},
	"kafka-topics":                     {kafkaScriptZookeeper, ""},
}
