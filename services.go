package main

import (
	"log"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"gopkg.in/yaml.v3"
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
	Registry         map[common.Address]map[common.Hash]*abi.Event
	Confirmations    *big.Int
}

type Config struct {
	Contracts []struct {
		Address    common.Address
		StartBlock *big.Int `yaml:"startBlock"`
		ABI        string
		Events     []string
	}
}

type Block struct {
	Hash   common.Hash
	Number *big.Int
}

var lastConf *Block = &Block{Number: big.NewInt(37850310)}

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
		Confirmations:    big.NewInt(5),
	}, nil
}

func (bl *BlockListener) CompileRegistryMap(configPath string) {
	var conf Config
	cb, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(cb, &conf)

	bl.Registry = make(map[common.Address]map[common.Hash]*abi.Event)
	for _, contract := range conf.Contracts {
		f, err := os.Open(contract.ABI)
		if err != nil {
			log.Fatal(err)
		}

		a, err := abi.JSON(f)
		f.Close()
		if err != nil {
			log.Fatal(err)
		}

		for _, event := range a.Events {
			if _, ok := bl.Registry[contract.Address]; !ok {
				bl.Registry[contract.Address] = make(map[common.Hash]*abi.Event)
			}
			bl.Registry[contract.Address][event.ID] = &event
		}
	}

}
