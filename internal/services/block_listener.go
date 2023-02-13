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
	"sync"
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

	ethtypes "github.com/ethereum/go-ethereum/core/types"
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
	ChainID          *big.Int
	Chain            string
	Logger           zerolog.Logger
	Producer         sarama.SyncProducer
	EventStreamTopic string
	Registry         map[common.Address]map[common.Hash]abi.Event
	Confirmations    *big.Int
	StartBlock       *big.Int
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
	Chains []ChainDetails
}

type ChainDetails struct {
	Chain      string   `yaml:"chain"`
	RPCURL     string   `yaml:"rpc_url"`
	StartBlock *big.Int `yaml:"startBlock"`
	Contracts  []contractDetails
}

type contractDetails struct {
	Address common.Address `yaml:"address"`
	ABI     string         `yaml:"abi"`
	Events  []string       `yaml:"events"`
}

type Event struct {
	EventName       string         `json:"eventName,omitempty"`
	Block           Block          `json:"block,omitempty"`
	Index           uint           `json:"index,omitempty"`
	Contract        string         `json:"contract,omitempty"`
	TransactionHash string         `json:"transactionHash,omitempty"`
	EventSignature  string         `json:"eventSignature,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	BlockCompleted  bool
}

type Block struct {
	Hash   common.Hash `json:"hash,omitempty"`
	Number *big.Int    `json:"number,omitempty"`
	Time   time.Time   `json:"time,omitempty"`
}

func NewBlockListener(s config.Settings, logger zerolog.Logger, producer sarama.SyncProducer, configPath, chain string) (BlockListener, error) {
	var conf Config
	cb, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatal(err)
	}

	err = yaml.Unmarshal(cb, &conf)
	if err != nil {
		log.Fatal(err)
	}

	var rpcURL string
	var contractDetails []contractDetails
	var startBlock *big.Int
	for _, c := range conf.Chains {
		if c.Chain != chain {
			continue
		}
		rpcURL = c.RPCURL + s.APIKey
		contractDetails = c.Contracts
		startBlock = c.StartBlock
	}

	c, err := ethclient.Dial(rpcURL)
	if err != nil {
		return BlockListener{}, err
	}

	pdb := db.NewDbConnectionFromSettings(context.TODO(), &s.DB, true)
	pdb.WaitForDB(logger)

	chainID, err := c.ChainID(context.Background())
	if err != nil {
		return BlockListener{}, err
	}

	b := BlockListener{
		Client:           c,
		EventStreamTopic: s.ContractEventTopic,
		Logger:           logger,
		Producer:         producer,
		Confirmations:    big.NewInt(int64(s.BlockConfirmations)),
		StartBlock:       startBlock,
		DB:               pdb,
		ChainID:          chainID,
		Chain:            chain,
	}

	b.CompileRegistryMap(contractDetails)

	return b, nil
}

func (bl *BlockListener) CompileRegistryMap(cd []contractDetails) {

	bl.Registry = make(map[common.Address]map[common.Hash]abi.Event)
	bl.ABIs = make(map[common.Address]abi.ABI)
	for _, contract := range cd {
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
			fmt.Printf("\tHead: %v\t Confirmed Head: %v", head.Number, confirmedHead)
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
				fmt.Printf("\tLatestBlockAdded: %v", latestBlockAdded.Int64())
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
func (bl *BlockListener) RecordBlock(head *ethtypes.Header) error {
	processedBlock := models.Block{
		ChainID: bl.ChainID.Int64(),
		Number:  head.Number.Int64(),
		Hash:    head.Hash().Bytes(),
	}
	return processedBlock.Upsert(context.Background(), bl.DB.DBS().Writer, true, []string{"chain_id"}, boil.Whitelist("number", "hash", "processed_at"), boil.Infer())
}

func (bl *BlockListener) GetBlockHead(blockNum *big.Int) (*ethtypes.Header, error) {
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

func (bl *BlockListener) GetFilteredBlockLogs(bHash common.Hash, contracts []common.Address) ([]ethtypes.Log, error) {
	timer := prometheus.NewTimer(metrics.FilteredLogsResponseTime)
	fil := ethereum.FilterQuery{
		BlockHash: &bHash,
		Addresses: bl.Contracts,
	}

	logs, err := bl.Client.FilterLogs(context.Background(), fil)
	if err != nil {
		metrics.FailedFilteredLogsFetch.Inc()
		return []ethtypes.Log{}, nil
	}
	timer.ObserveDuration()
	metrics.SuccessfulFilteredLogsFetch.Inc()
	return logs, err
}

func (bl *BlockListener) ChainIndexer(blockNum *big.Int, wg sync.WaitGroup) {
	defer wg.Done()
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
				Source:  fmt.Sprintf("%v/%v", bl.Chain, bl.ChainID),
				Subject: vLog.Address.String(),
				Type:    "zone.dimo.contract.event.v2",
				Time:    tm,
				Data: Event{
					EventName: ev.Name,
					Block: Block{
						Number: head.Number,
						Hash:   head.Hash(),
						Time:   tm},
					Index:           vLog.Index,
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
				metrics.KafkaEventMessageFailedToSend.Inc()
			}

			metrics.EventsEmitted.Inc()
			metrics.KafkaEventMessageSent.Inc()

		}
	}

	event := shared.CloudEvent[Event]{
		ID:     ksuid.New().String(),
		Source: fmt.Sprintf("%v/%v", bl.Chain, bl.ChainID),
		Type:   "zone.dimo.blockchain.block.processed.v2",
		Time:   tm,
		Data: Event{
			Block: Block{
				Number: head.Number,
				Hash:   head.Hash(),
				Time:   tm},
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
