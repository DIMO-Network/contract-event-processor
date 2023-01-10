package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
	"github.com/volatiletech/null/v8"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"gopkg.in/yaml.v3"

	_ "github.com/lib/pq"

	"event-stream/models"
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
	PostgresUser       string   `yaml:"POSTGRES_USER"`
	PostgresPassword   string   `yaml:"POSTGRES_PASSWORD"`
	PostgresDB         string   `yaml:"POSTGRES_DB"`
	PostgresHOST       string   `yaml:"POSTGRES_HOST"`
	PostgresPort       int      `yaml:"POSTGRES_PORT"`
}

type BlockListener struct {
	Client           *ethclient.Client
	Contracts        []common.Address
	Logger           zerolog.Logger
	Producer         sarama.SyncProducer
	EventStreamTopic string
	Registry         map[common.Address]map[common.Hash]*abi.Event
	Confirmations    *big.Int
	DB               *sql.DB
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

func NewBlockListener(s Settings, logger zerolog.Logger, producer sarama.SyncProducer) (BlockListener, error) {
	c, err := ethclient.Dial(s.WebSocketAddress + s.AlchemyAPIKey)
	if err != nil {
		return BlockListener{}, err
	}

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		s.PostgresHOST, s.PostgresPort, s.PostgresUser, s.PostgresPassword, s.PostgresDB)

	pg, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return BlockListener{}, err
	}
	err = pg.Ping()
	if err != nil {
		return BlockListener{}, err
	}

	return BlockListener{
		Client:           c,
		EventStreamTopic: s.EventStreamTopic,
		Logger:           logger,
		Producer:         producer,
		Confirmations:    big.NewInt(14), // pulled this standard from coinbase
		DB:               pg,
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
		bl.Contracts = append(bl.Contracts, contract.Address)
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

// fetch the most recently indexed block or return latest block
func (bl *BlockListener) GetBlock(blockNum *big.Int) (Block, error) {
	latestBlock := Block{
		Number: new(big.Int),
	}

	if blockNum != nil {
		return bl.GetBlockByNumber(blockNum)
	}

	resp, err := models.Blocks(qm.OrderBy(models.BlockColumns.Number+" DESC")).One(context.Background(), bl.DB)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return bl.GetBlockByNumber(nil)
		}
		return Block{}, err
	}

	latestBlock.Number = big.NewInt(resp.Number)
	latestBlock.Hash = common.HexToHash(resp.Hash.String)

	return latestBlock, nil
}

func (bl *BlockListener) GetBlockByNumber(blockNum *big.Int) (Block, error) {
	latestBlock := Block{
		Number: new(big.Int),
	}

	head, err := bl.Client.HeaderByNumber(context.Background(), blockNum)
	if err != nil {
		return Block{}, err
	}
	latestBlock.Number = head.Number
	latestBlock.Hash = head.Hash()
	return latestBlock, nil
}

// fetch the current block that hasn't yet been indexed
func (bl *BlockListener) GetNextBlock(block *types.Header) (*types.Header, error) {
	return bl.Client.HeaderByNumber(context.Background(), block.Number.Add(block.Number, big.NewInt(1)))
}

// fetch the current block that hasn't yet been indexed
func (bl *BlockListener) RecordBlock(block *types.Header) error {
	processedBlock := models.Block{
		Number:    block.Number.Int64(),
		Hash:      null.StringFrom(block.Hash().String()),
		Processed: null.BoolFrom(true),
	}

	return processedBlock.Insert(context.Background(), bl.DB, boil.Infer())
}

func (bl *BlockListener) ChainIndexer(blockNum *big.Int) {

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Running...")

	block, err := bl.GetBlock(blockNum)
	if err != nil {
		bl.Logger.Fatal().Int64("block number", blockNum.Int64()).Msgf("error fetching block: %v", err)
	}

	head, err := bl.Client.HeaderByNumber(context.Background(), new(big.Int).Sub(block.Number, bl.Confirmations))
	if err != nil {
		bl.Logger.Fatal().Int64("block number", blockNum.Int64()).Msgf("error fetching block head: %v", err)
	}

	bl.Logger.Info().Str("Block Head", block.Number.String()).Str("Currently Processing", head.Number.String()).Msg("Processing at head minus 14 to ensure confirmed transactions")

	for {
		select {
		case <-tick.C:
			err = bl.ProcessBlock(bl.Client, head)
			if err != nil {
				log.Fatal(err)
			}

			err = bl.RecordBlock(head)
			if err != nil {
				log.Fatal(err)
			}
			head, err = bl.GetNextBlock(head)
			if err != nil {
				log.Fatal(err)
			}

		case sig := <-sigChan:
			log.Printf("Received signal, terminating: %s", sig)
			return
		}
	}

}

func (bl *BlockListener) ProcessBlock(client *ethclient.Client, head *types.Header) error {
	log.Printf("Processing block %s", head.Number)
	blockHash := head.Hash()

	fil := ethereum.FilterQuery{
		BlockHash: &blockHash,
		Addresses: bl.Contracts,
	}
	logs, err := client.FilterLogs(context.Background(), fil)
	if err != nil {
		return err
	}

	for _, vLog := range logs {
		if vLog.Removed {
			bl.Logger.Info().Uint64("Block Number", vLog.BlockNumber).Msg("Block removed due to chain reorganization")
		}

		if ev, ok := bl.Registry[vLog.Address][vLog.Topics[0]]; ok {

			event := shared.CloudEvent[Event]{
				ID:      ksuid.New().String(),
				Source:  string(vLog.Address.String()),
				Subject: vLog.TxHash.String(),
				Time:    time.Now().UTC(),
				Data: Event{
					Contract:        vLog.Address.String(),
					TransactionHash: vLog.TxHash.String(),
					EventSignature:  vLog.Topics[0].String(),
				}}

			event.Data.Arguments = make(map[string]any)
			err = ev.Inputs.UnpackIntoMap(event.Data.Arguments, vLog.Data)

			var indexed abi.Arguments
			for _, arg := range ev.Inputs {
				if arg.Indexed {
					indexed = append(indexed, arg)
				}
			}
			// TODO AE-- topic slice looks odd, was getting mismatched length error before
			// this needs to be improved
			err = abi.ParseTopicsIntoMap(event.Data.Arguments, indexed, vLog.Topics[len(vLog.Topics)-len(indexed):])
			if err != nil {
				log.Fatal(err)
			}

			eBytes, _ := json.Marshal(event)
			message := &sarama.ProducerMessage{Topic: bl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
			_, _, err := bl.Producer.SendMessage(message)
			if err != nil {
				bl.Logger.Info().Str(bl.EventStreamTopic, ev.Name).Msgf("error sending event to stream: %v", err)
			}

		}
	}

	event := shared.CloudEvent[Event]{
		ID:      ksuid.New().String(),
		Subject: blockHash.String(),
		Time:    time.Now().UTC(),
		Data: Event{
			BlockCompleted: true,
		}}

	eBytes, _ := json.Marshal(event)
	message := &sarama.ProducerMessage{Topic: bl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
	_, _, err = bl.Producer.SendMessage(message)
	if err != nil {
		bl.Logger.Info().Str("Block", head.Number.String()).Msgf("error sending block completion confirmation: %v", err)
	}

	return nil
}
