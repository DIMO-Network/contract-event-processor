package config

import "github.com/DIMO-Network/shared/db"

type Settings struct {
	EthereumRPCURL     string `yaml:"ETHEREUM_RPC_URL"`
	BlockConfirmations int    `yaml:"BLOCK_CONFIRMATIONS"`
	ContractEventTopic string `yaml:"CONTRACT_EVENT_TOPIC"`

	KafkaBrokers string `yaml:"KAFKA_BROKERS"`

	DB db.Settings `yaml:"DB"`

	MonitoringPort string `yaml:"MONITORING_PORT"`
}
