package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"os"
	"os/signal"
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

func (bl *BlockListener) PollNewBlocks(blockNum *big.Int, c chan *big.Int, sigChan chan os.Signal) {

	// should this be one second now that we're waiting?
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	latestBlockAdded, err := bl.FetchStartingBlock(blockNum)
	if err != nil {
		bl.Logger.Err(err).Msg("error fetching starting block")
	}

	for {
		select {
		case <-tick.C:
			head, err := bl.Client.HeaderByNumber(context.Background(), nil)
			if err != nil {
				bl.Logger.Err(err).Msg("error head of blockchain")
			}
			confirmedHead := new(big.Int).Sub(head.Number, bl.Confirmations)

			if latestBlockAdded == nil {
				c <- confirmedHead
				latestBlockAdded = confirmedHead
				continue
			}

			for confirmedHead.Cmp(latestBlockAdded) > 0 {
				c <- new(big.Int).Add(latestBlockAdded, big.NewInt(1))
				latestBlockAdded = new(big.Int).Add(latestBlockAdded, big.NewInt(1))
			}
		case sig := <-sigChan:
			log.Printf("Received signal, terminating: %s", sig)
			close(c)
			return
		}
	}
}

func (bl *BlockListener) FetchStartingBlock(blockNum *big.Int) (*big.Int, error) {

	if blockNum != nil {
		return blockNum, nil
	}

	resp, err := models.Blocks(qm.OrderBy(models.BlockColumns.Number+" DESC")).One(context.Background(), bl.DB.DBS().Reader)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return big.NewInt(resp.Number), nil
}

// RecordBlock store block number and hash after processing
func (bl *BlockListener) RecordBlock(head *types.Header) error {
	processedBlock := models.Block{
		Number: head.Number.Int64(),
		Hash:   head.Hash().Bytes(),
	}

	// on conflict, update the time that the block was processed at
	return processedBlock.Upsert(context.Background(), bl.DB.DBS().Writer, true, []string{"number"}, boil.Whitelist("processed_at"), boil.Infer())
}

func (bl *BlockListener) ChainIndexer(blockNum *big.Int) {

	bl.Logger.Info().Msg("chain indexer starting")
	blockNumChannel := make(chan *big.Int)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go bl.PollNewBlocks(blockNum, blockNumChannel, sigChan)

	for block := range blockNumChannel {
		err := bl.ProcessBlock(block)
		if err != nil {
			bl.Logger.Err(err).Msg("error processing blocks")
		}
	}

}

func (bl *BlockListener) ProcessBlock(blockNum *big.Int) error {
	bl.Logger.Debug().Int64("blockNumber", blockNum.Int64()).Msg("processing")
	head, err := bl.Client.HeaderByNumber(context.Background(), blockNum)
	if err != nil {
		return err
	}

	blockHash := head.Hash()
	tm := time.Unix(int64(head.Time), 0)

	fil := ethereum.FilterQuery{
		BlockHash: &blockHash,
		Addresses: bl.Contracts,
	}
	logs, err := bl.Client.FilterLogs(context.Background(), fil)
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

	return bl.RecordBlock(head)
}
