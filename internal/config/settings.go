package config

import "github.com/DIMO-Network/shared/db"

type Settings struct {
	Environment string `yaml:"ENVIRONMENT"`

	BlockConfirmations int64  `yaml:"BLOCK_CONFIRMATIONS"`
	ContractEventTopic string `yaml:"CONTRACT_EVENT_TOPIC"`

	KafkaBrokers string `yaml:"KAFKA_BROKERS"`

	DB db.Settings `yaml:"DB"`

	MonitoringPort string `yaml:"MONITORING_PORT"`

	BlockchainRPCURL string `yaml:"BLOCKCHAIN_RPC_URL"`

	StartingBlock int64 `yaml:"STARTING_BLOCK"`

	DIMORegistryAddress string `yaml:"DIMO_REGISTRY_ADDRESS"`
	RelayAddresses      string `yaml:"RELAY_ADDRESSES"`
}
