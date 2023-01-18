package config

import "github.com/DIMO-Network/shared/db"

type Settings struct {
	EthereumRPCURL string `yaml:"ETHEREUM_RPC_URL"`

	BlockConfirmations int `yaml:"BLOCK_CONFIRMATIONS"`

	EventStreamTopic string `yaml:"EVENT_STREAM_TOPIC"`

	KafkaBroker string `yaml:"KAFKA_BROKER"`
	Partitions  int    `yaml:"PARTITIONS"`

	DB db.Settings `yaml:"DB"`

	MonitoringPort string `yaml:"MONITORING_PORT"`
}
