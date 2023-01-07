package main

import (
	"github.com/ethereum/go-ethereum/ethclient"
)

type Settings struct {
	Contracts          []string `yaml:"CONTRACTS"`
	WebSocketAddress   string   `yaml:"WEB_SOCKET_ADDRESS"`
	AlchemyAPIKey      string   `yaml:"API_KEY"`
	DIMORewardContract string   `yaml:"DIMO_REWARDS_CONTRACT"`
	EventStreamTopic   string   `yaml:"EVENT_STREAM_TOPIC"`
	Partitions         int      `yaml:"PARTITIONS"`
	KafkaBroker        string   `yaml:"KAFKA_BROKER"`
	RandomContract     string   `yaml:"RANDOM_CONTRACT"`
}

type BlockListener struct {
	Client           *ethclient.Client
	Contracts        []string
	RewardContract   string
	RandomContract   string
	EventStreamTopic string
}

func NewBlockListener(s Settings) (BlockListener, error) {
	c, err := ethclient.Dial(s.WebSocketAddress + s.AlchemyAPIKey)
	if err != nil {
		return BlockListener{}, err
	}

	return BlockListener{
		Client:           c,
		Contracts:        s.Contracts,
		RewardContract:   s.DIMORewardContract,
		RandomContract:   s.RandomContract,
		EventStreamTopic: s.EventStreamTopic,
	}, nil
}
