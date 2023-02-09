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
	"github.com/DIMO-Network/contract-event-processor/internal/infrastructure/metrics"
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
	"github.com/prometheus/client_golang/prometheus"
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
	Limit            int
	DevTest          bool
}

type Config struct {
	Contracts []struct {
		Address    common.Address
		StartBlock *big.Int `yaml:"startBlock"`
		ABI        string
		Chain      string
		Events     []string
	}
}

type Block struct {
	Hash   common.Hash
	Number *big.Int
}

func NewBlockListener(s config.Settings, logger zerolog.Logger, producer sarama.SyncProducer, chain string) (BlockListener, error) {

	rpcURL := s.PolygonRPCURL

	if chain == "ethereum" {
		rpcURL = s.EthereumRPCURL
	}
	if chain == "mumbai" {
		rpcURL = s.MumbaiRPCURL
	}

	c, err := ethclient.Dial(rpcURL)
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

func (bl *BlockListener) CompileRegistryMap(configPath, chain string) {
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
		if contract.Chain != chain {
			continue
		}
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
				metrics.BlocksInQueue.Set(1)
				c <- confirmedHead
				latestBlockAdded = confirmedHead
				continue
			}

			for confirmedHead.Cmp(latestBlockAdded) > 0 {
				metrics.BlocksInQueue.Set(float64(new(big.Int).Sub(confirmedHead, latestBlockAdded).Int64()))
				c <- new(big.Int).Add(latestBlockAdded, big.NewInt(1))
				latestBlockAdded = new(big.Int).Add(latestBlockAdded, big.NewInt(1))
			}
		case sig := <-sigChan:
			bl.Logger.Info().Msgf("Received signal, terminating: %s", sig)
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

func (bl *BlockListener) GetBlockHead(blockNum *big.Int) (*types.Header, error) {
	timer := prometheus.NewTimer(metrics.AlchemyHeadPollResponseTime)
	head, err := bl.Client.HeaderByNumber(context.Background(), blockNum)
	if err != nil {
		metrics.FailedHeadPolls.Inc()
		return nil, err
	}
	timer.ObserveDuration()
	metrics.SuccessfulHeadPolls.Inc()

	return head, err
}

func (bl *BlockListener) GetFilteredBlockLogs(bHash common.Hash, contracts []common.Address) ([]types.Log, error) {
	timer := prometheus.NewTimer(metrics.FilteredLogsResponseTime)
	fil := ethereum.FilterQuery{
		BlockHash: &bHash,
		Addresses: bl.Contracts,
	}

	logs, err := bl.Client.FilterLogs(context.Background(), fil)
	if err != nil {
		metrics.FailedFilteredLogsFetch.Inc()
		return []types.Log{}, nil
	}
	timer.ObserveDuration()
	metrics.SuccessfulFilteredLogsFetch.Inc()
	return logs, err
}

func (bl *BlockListener) ChainIndexer(blockNum *big.Int) {
	bl.Logger.Info().Msg("chain indexer starting")
	blockNumChannel := make(chan *big.Int)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go bl.PollNewBlocks(blockNum, blockNumChannel, sigChan)

	for block := range blockNumChannel {
		if bl.Limit < 1 && bl.DevTest {
			return
		}
		err := bl.ProcessBlock(block)
		if err != nil {
			bl.Logger.Err(err).Msg("error processing blocks")
		}
		bl.Limit--
	}
}

func (bl *BlockListener) ProcessBlock(blockNum *big.Int) error {
	bl.Logger.Debug().Int64("blockNumber", blockNum.Int64()).Msg("processing")
	head, err := bl.GetBlockHead(blockNum)
	if err != nil {
		return err
	}

	tm := time.Unix(int64(head.Time), 0)

	logs, err := bl.GetFilteredBlockLogs(head.Hash(), bl.Contracts)
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
					Index:           vLog.Index,
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
				metrics.KafkaEventMessageFailedToSend.Inc()
			}

			metrics.EventsEmitted.Inc()
			metrics.KafkaEventMessageSent.Inc()

		}
	}

	event := shared.CloudEvent[Event]{
		ID:      ksuid.New().String(),
		Source:  head.Number.String(),
		Subject: head.Hash().String(),
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

	err = bl.RecordBlock(head)
	if err != nil {
		metrics.ProcessedBlockNumberStoreFailed.Inc()
		return err
	}

	metrics.ProcessedBlockNumberStored.Inc()
	metrics.BlocksProcessed.Inc()
	metrics.BlocksInQueue.Dec()

	return nil
}
