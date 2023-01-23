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
	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v3"
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

func (bl *BlockListener) BeginProcessingAtHead(blockNum *big.Int) ([]*big.Int, error) {

	head, err := bl.Client.HeaderByNumber(context.Background(), blockNum)
	if err != nil {
		return []*big.Int{}, err
	}

	startBlock := big.NewInt(0).Sub(head.Number, bl.Confirmations)

	if blockNum != nil {
		bl.Logger.Info().Int64("processingFrom", startBlock.Int64()).Int64("overrideValue", blockNum.Int64()).Msgf("processing will begin at %v less than override value", bl.Confirmations)
		nextBlocks := getRange(head.Number, startBlock)
		return nextBlocks, nil
	}

	resp, err := models.Blocks(qm.OrderBy(models.BlockColumns.Number+" DESC")).One(context.Background(), bl.DB.DBS().Reader)
	if err != nil || resp == nil {
		if errors.Is(err, sql.ErrNoRows) || resp == nil {
			bl.Logger.Info().Int64("processingFrom", startBlock.Int64()).Msgf("no value found in database, processing will begin at %v less than current chain head", bl.Confirmations)
			nextBlocks := getRange(head.Number, startBlock)
			return nextBlocks, nil
		}
		return []*big.Int{}, err
	}

	bl.Logger.Info().Int64("processingFrom", resp.Number).Msg("processing will begin at latest number found in database")
	nextBlocks := getRange(head.Number, big.NewInt(resp.Number))
	return nextBlocks, nil
}

// fetch the most recently indexed block or return latest block
func (bl *BlockListener) GetBlockHead(currentHead *big.Int, blocks []*big.Int, lastProcessed *big.Int) ([]*big.Int, error) {

	var head *types.Header
	var err error

	head, err = bl.Client.HeaderByNumber(context.Background(), currentHead)
	if err != nil {
		return []*big.Int{}, err
	}

	if len(blocks) >= 1 {
		if blocks[len(blocks)-1] == head.Number && len(blocks) >= int(bl.Confirmations.Int64()) {
			idx := slices.IndexFunc(blocks, func(c *big.Int) bool { return c == lastProcessed })
			blocks = blocks[idx+1:]
			return blocks, nil
		}
	}

	nextBlocks := getRange(head.Number, lastProcessed)
	return nextBlocks, nil
}

// fetch the current block that hasn't yet been indexed
func (bl *BlockListener) GetNextBlock(block *types.Header) (*types.Header, error) {
	nextBlock := new(big.Int).Add(block.Number, big.NewInt(1))
	head, err := bl.Client.HeaderByNumber(context.Background(), nextBlock)

	if err == ethereum.NotFound {
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

	// on conflict, update the time that the block was processed at
	return processedBlock.Upsert(context.Background(), bl.DB.DBS().Writer, true, []string{"number"}, boil.Whitelist("processed_at"), boil.Infer())
}

func (bl *BlockListener) ChainIndexer(blockNum *big.Int) {

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Running...")

	var lastProcessedBlock *big.Int
	unprocessedBlocks, err := bl.BeginProcessingAtHead(blockNum)
	if err != nil {
		bl.Logger.Fatal().Int64("block number", blockNum.Int64()).Msgf("error fetching block head: %v", err)
	}

	for {
		select {
		case <-tick.C:

			for _, block := range unprocessedBlocks {
				fmt.Println("\t\t\tProcessing: ", block.Int64())
				head, err := bl.Client.HeaderByNumber(context.Background(), block)
				if err != nil {
					log.Fatal(err)
				}

				err = bl.ProcessBlock(bl.Client, head)
				if err != nil {
					log.Fatal(err)
				}

				err = bl.RecordBlock(head)
				if err != nil {
					log.Fatal(err)
				}
				lastProcessedBlock = block
			}

			unprocessedBlocks, err = bl.GetBlockHead(nil, unprocessedBlocks, lastProcessedBlock)
			if err != nil {
				bl.Logger.Fatal().Int64("block number", blockNum.Int64()).Msgf("error fetching block head: %v", err)
			}

		case sig := <-sigChan:
			log.Printf("Received signal, terminating: %s", sig)
			return
		}
	}

}

func (bl *BlockListener) ProcessBlock(client *ethclient.Client, head *types.Header) error {
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

func getRange(currentHead *big.Int, lastProcessed *big.Int) []*big.Int {
	// get all numbers in range, inclusive
	difference := big.NewInt(0).Sub(currentHead, lastProcessed).Int64()
	r := make([]*big.Int, difference)
	for i := range r {
		r[i] = big.NewInt(0).Add(lastProcessed, big.NewInt(int64(i)+1))
	}
	return r
}
