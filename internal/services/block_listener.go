package services

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
	"strconv"
	"syscall"
	"time"

	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/DIMO-Network/contract-event-processor/models"
	"github.com/DIMO-Network/shared"
	"github.com/DIMO-Network/shared/db"
	"github.com/Shopify/sarama"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"gopkg.in/yaml.v3"

	_ "github.com/lib/pq"
)

type BlockListener struct {
	Client           *ethclient.Client
	Contracts        []common.Address
	Logger           zerolog.Logger
	Producer         sarama.SyncProducer
	EventStreamTopic string
	Registry         map[common.Address]map[common.Hash]abi.Event
	Confirmations    *big.Int
	DB               db.Store
	ABIs             map[common.Address]abi.ABI
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

func NewBlockListener(s config.Settings, logger zerolog.Logger, producer sarama.SyncProducer) (BlockListener, error) {
	c, err := ethclient.Dial(s.EthereumRPCURL)
	if err != nil {
		return BlockListener{}, err
	}

	pdb := db.NewDbConnectionFromSettings(context.TODO(), &s.DB, true)
	pdb.WaitForDB(logger)

	return BlockListener{
		Client:           c,
		EventStreamTopic: s.ContractEventTopic,
		Logger:           logger,
		Producer:         producer,
		Confirmations:    big.NewInt(int64(s.BlockConfirmations)),
		DB:               pdb,
	}, nil
}

func (bl *BlockListener) CompileRegistryMap(configPath string) {
	var conf Config
	cb, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(cb, &conf)
	if err != nil {
		log.Fatal(err)
	}

	bl.Registry = make(map[common.Address]map[common.Hash]abi.Event)
	bl.ABIs = make(map[common.Address]abi.ABI)
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

		bl.Logger.Info().Str("address", contract.Address.String()).Str("abiFile", contract.ABI).Msg("Watching contract.")

		bl.ABIs[contract.Address] = a
		bl.Registry[contract.Address] = make(map[common.Hash]abi.Event)

		for _, event := range a.Events {
			bl.Registry[contract.Address][event.ID] = event
		}
	}

}

// fetch the most recently indexed block or return latest block
func (bl *BlockListener) GetBlockHead(blockNum *big.Int) (*types.Header, error) {

	if blockNum != nil {
		bl.Logger.Info().Int64("block number", blockNum.Int64()).Msgf("setting block head to passed value: %v", blockNum.Int64())
		return bl.Client.HeaderByNumber(context.Background(), blockNum)
	}

	resp, err := models.Blocks(qm.OrderBy(models.BlockColumns.Number+" DESC")).One(context.Background(), bl.DB.DBS().Reader)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			bl.Logger.Info().Msg("no value passed or found in db; setting block head to current head minus five")
			h, e := bl.Client.HeaderByNumber(context.Background(), nil)
			if e != nil {
				return nil, e
			}
			return bl.Client.HeaderByNumber(context.Background(), new(big.Int).Sub(h.Number, bl.Confirmations))
		}
		return nil, err
	}

	bl.Logger.Info().Int64("block number", resp.Number).Msgf("setting block head to most recently processed, found in db plus one: %v", resp.Number+1)
	return bl.Client.HeaderByNumber(context.Background(), new(big.Int).Add(big.NewInt(resp.Number), big.NewInt(1)))
}

// fetch the current block that hasn't yet been indexed
func (bl *BlockListener) GetNextBlock(block *types.Header) (*types.Header, error) {
	nextBlock := new(big.Int).Add(block.Number, big.NewInt(1))
	head, err := bl.Client.HeaderByNumber(context.Background(), nextBlock)

	if err == ethereum.NotFound && head == nil {
		time.Sleep(2 * time.Second)
		head, err = bl.GetNextBlock(block)
	}

	return head, err
}

// fetch the current block that hasn't yet been indexed
func (bl *BlockListener) RecordBlock(block *types.Header) error {
	processedBlock := models.Block{
		Number: block.Number.Int64(),
		Hash:   block.Hash().Bytes(),
	}

	return processedBlock.Insert(context.Background(), bl.DB.DBS().Writer, boil.Infer())
}

func (bl *BlockListener) ChainIndexer(blockNum *big.Int) {

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Running...")

	head, err := bl.GetBlockHead(blockNum)
	if err != nil {
		bl.Logger.Fatal().Int64("block number", blockNum.Int64()).Msgf("error fetching block head: %v", err)
	}

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
	t, err := strconv.ParseInt(strconv.Itoa(int(head.Time)), 10, 64)
	if err != nil {
		return err
	}
	tm := time.Unix(t, 0).UTC()

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
			bl.Logger.Info().Uint64("blockNumber", vLog.BlockNumber).Msg("Block removed due to chain reorganization")
		}

		if ev, ok := bl.Registry[vLog.Address][vLog.Topics[0]]; ok {
			event := shared.CloudEvent[Event]{
				ID:      ksuid.New().String(),
				Source:  head.Number.String(),
				Subject: vLog.TxHash.String(),
				Type:    "zone.dimo.contract.event",
				Time:    tm,
				Data: Event{
					EventName:       ev.Name,
					Contract:        vLog.Address.String(),
					TransactionHash: vLog.TxHash.String(),
					EventSignature:  vLog.Topics[0].String(),
					Arguments:       make(map[string]any),
				}}

			var indexed abi.Arguments
			for _, arg := range ev.Inputs {
				if arg.Indexed {
					indexed = append(indexed, arg)
				}
			}

			err := bl.ABIs[vLog.Address].UnpackIntoMap(event.Data.Arguments, ev.Name, vLog.Data)
			if err != nil {
				bl.Logger.Info().Str(bl.EventStreamTopic, ev.Name).Str("blockNumber", head.Number.String()).Str("contract", vLog.Address.String()).Str("event", vLog.TxHash.String()).Msg("unable to parse non-indexed arguments")
			}

			err = abi.ParseTopicsIntoMap(event.Data.Arguments, indexed, vLog.Topics[1:])
			if err != nil {
				bl.Logger.Info().Str(bl.EventStreamTopic, ev.Name).Str("blockNumber", head.Number.String()).Str("contract", vLog.Address.String()).Str("event", vLog.TxHash.String()).Msg("unable to parse indexed arguments")
			}

			eBytes, _ := json.Marshal(event)
			message := &sarama.ProducerMessage{Topic: bl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
			_, _, err = bl.Producer.SendMessage(message)
			if err != nil {
				bl.Logger.Info().Str(bl.EventStreamTopic, ev.Name).Str("blockNumber", head.Number.String()).Str("contract", vLog.Address.String()).Str("event", vLog.TxHash.String()).Msgf("error sending event to stream: %v", err)
			}

		}
	}

	event := shared.CloudEvent[Event]{
		ID:      ksuid.New().String(),
		Source:  head.Number.String(),
		Subject: blockHash.String(),
		Type:    "zone.dimo.blockchain.block.processed",
		Time:    tm,
		Data: Event{
			BlockCompleted: true,
		}}

	eBytes, _ := json.Marshal(event)
	message := &sarama.ProducerMessage{Topic: bl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
	_, _, err = bl.Producer.SendMessage(message)
	if err != nil {
		bl.Logger.Info().Str("Block", head.Number.String()).Msgf("error sending block completion confirmation: %v", err)
		return err
	}

	return nil
}
