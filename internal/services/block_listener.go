package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/DIMO-Network/contract-event-processor/internal/infrastructure/metrics"
	"github.com/DIMO-Network/contract-event-processor/models"
	"github.com/DIMO-Network/shared"
	"github.com/DIMO-Network/shared/db"
	"github.com/IBM/sarama"
	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
	"github.com/volatiletech/sqlboiler/v4/boil"
	"github.com/volatiletech/sqlboiler/v4/queries/qm"
)

// MaxRetries maximum number of times we will try to call alchemy before returning error
const MaxRetries int = 5

// RetryDuration period to wait before retrying alchemy call
const RetryDuration time.Duration = time.Second * 2

type BlockListener struct {
	client           *ethclient.Client
	contracts        []common.Address
	chainID          int64
	logger           zerolog.Logger
	producer         sarama.SyncProducer
	eventStreamTopic string
	confirmations    *big.Int
	db               db.Store
	StartBlock       *big.Int
	ABIs             map[common.Address]abi.ABI
	Limit            int
	DevTest          bool
	registryAddress  *common.Address
	relayAddresses   []common.Address
}

type ChainDetails struct {
	Chain     string            `yaml:"chain"`
	Contracts []contractDetails `yaml:"contracts"`
}

type contractDetails struct {
	Address common.Address `yaml:"address"`
	ABI     string         `yaml:"abi"`
}

type Event struct {
	ChainID             int64          `json:"chainId"`
	EventName           string         `json:"eventName,omitempty"`
	Block               Block          `json:"block,omitempty"`
	Index               uint           `json:"index,omitempty"`
	Contract            common.Address `json:"contract,omitempty"`
	TransactionHash     common.Hash    `json:"transactionHash,omitempty"`
	EventSignature      common.Hash    `json:"eventSignature,omitempty"`
	Arguments           map[string]any `json:"arguments,omitempty"`
	BlockCompleted      bool           `json:"blockCompleted"`
	FromMetaTransaction bool           `json:"fromMetaTransaction"`
}

type Block struct {
	Number *big.Int    `json:"number,omitempty"`
	Hash   common.Hash `json:"hash,omitempty"`
	Time   time.Time   `json:"time,omitempty"`
}

func retry(f func() error, retry int, wait time.Duration) error {
	e := []error{}
	for i := 0; i < retry; i++ {
		err := f()
		if err == nil {
			return nil
		}
		e = append(e, err)

		time.Sleep(wait)

	}
	return errors.Join(e...)
}

func NewBlockListener(s config.Settings, logger zerolog.Logger, producer sarama.SyncProducer, configPath string) (*BlockListener, error) {
	conf, err := shared.LoadConfig[ChainDetails](configPath)
	if err != nil {
		return nil, err
	}

	c, err := ethclient.Dial(s.BlockchainRPCURL)
	if err != nil {
		return nil, err
	}

	pdb := db.NewDbConnectionFromSettings(context.TODO(), &s.DB, true)
	pdb.WaitForDB(logger)

	chainID, err := c.ChainID(context.Background())
	if err != nil {
		return nil, err
	}

	var registryAddress *common.Address
	var relayAddresses []common.Address
	if s.DIMORegistryAddress != "" {
		if !common.IsHexAddress(s.DIMORegistryAddress) {
			return nil, fmt.Errorf("given registry address %q is not an address", s.DIMORegistryAddress)
		}

		ra := common.HexToAddress(s.DIMORegistryAddress)
		registryAddress = &ra

		strRelays := strings.Split(s.RelayAddresses, ",")
		relayAddresses = make([]common.Address, len(strRelays))
		for i, sa := range strRelays {
			if !common.IsHexAddress(sa) {
				return nil, fmt.Errorf("given relay address %q is not an address", sa)
			}
			relayAddresses[i] = common.HexToAddress(sa)
		}
	}

	if !chainID.IsInt64() {
		return nil, fmt.Errorf("chain id %s cannot fit in an int64", chainID)
	}
	var sb *big.Int
	if s.StartingBlock != 0 {
		sb = big.NewInt(s.StartingBlock)
		logger.Info().Msgf("Read STARTING_BLOCK value %d.", s.StartingBlock)
	}

	ctrs := []common.Address{}
	for _, x := range conf.Contracts {
		ctrs = append(ctrs, x.Address)
	}

	b := BlockListener{
		client:           c,
		eventStreamTopic: s.ContractEventTopic,
		logger:           logger,
		producer:         producer,
		confirmations:    big.NewInt(s.BlockConfirmations),
		db:               pdb,
		chainID:          chainID.Int64(),
		StartBlock:       sb,
		contracts:        ctrs,
		Limit:            10,
		registryAddress:  registryAddress,
		relayAddresses:   relayAddresses,
	}

	b.CompileRegistryMap(conf.Contracts)

	return &b, nil
}

func (bl *BlockListener) CompileRegistryMap(cd []contractDetails) {
	bl.ABIs = make(map[common.Address]abi.ABI)
	for _, contract := range cd {
		func() {
			bl.contracts = append(bl.contracts, contract.Address)
			f, err := os.Open(contract.ABI)
			if err != nil {
				bl.logger.Fatal().Err(err).Msgf("Couldn't open ABI %s.", contract.ABI)
			}
			defer f.Close()

			bl.ABIs[contract.Address], err = abi.JSON(f)
			if err != nil {
				bl.logger.Fatal().Err(err).Msgf("Couldn't parse ABI %s.", contract.ABI)
			}

			bl.logger.Info().Int64("chain", bl.chainID).Str("address", contract.Address.String()).Str("abiFile", contract.ABI).Msg("Watching contract.")
		}()
	}
}

func (bl *BlockListener) PollNewBlocks(ctx context.Context, blockNum *big.Int, c chan<- *big.Int) error {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	latestBlockAdded, err := bl.FetchStartingBlock(blockNum)
	if err != nil {
		bl.logger.Err(err).Int64("chain", bl.chainID).Msg("error fetching starting block")
	}

	for {
		select {
		case <-tick.C:
			var head uint64
			err := retry(func() error {
				var err error
				head, err = bl.client.BlockNumber(context.Background())
				if err != nil {
					metrics.CountRetries.With(prometheus.Labels{"type": "BlockNumber"}).Inc()
				}
				return err
			}, MaxRetries, RetryDuration)
			if err != nil {
				bl.logger.Err(err).Int64("chain", bl.chainID).Msg("error fetching head")
			}
			metrics.CountRetries.With(prometheus.Labels{"type": "BlockNumber"}).Set(0)
			confirmedHead := new(big.Int).Sub(big.NewInt(int64(head)), bl.confirmations)
			if latestBlockAdded == nil {
				metrics.BlocksInQueue.Set(1)
				c <- confirmedHead
				latestBlockAdded = confirmedHead
				continue
			}

			for confirmedHead.Cmp(latestBlockAdded) > 0 {
				metrics.BlocksInQueue.Set(float64(new(big.Int).Sub(confirmedHead, latestBlockAdded).Int64()))
				nextBlock := new(big.Int).Add(latestBlockAdded, big.NewInt(1))
				select {
				case c <- nextBlock:
					latestBlockAdded = nextBlock
				case <-ctx.Done():
					return nil
				}

			}
		case <-ctx.Done():
			bl.logger.Info().Int64("chain", bl.chainID).Msg("Closing channel")
			return nil
		}

	}
}

func (bl *BlockListener) FetchStartingBlock(blockNum *big.Int) (*big.Int, error) {
	if blockNum != nil {
		bl.logger.Info().Msgf("Starting from configured block %d.", blockNum)
		return blockNum, nil
	}

	resp, err := models.Blocks(
		models.BlockWhere.ChainID.EQ(bl.chainID),
		qm.OrderBy(models.BlockColumns.Number+" DESC")).
		One(context.Background(), bl.db.DBS().Reader)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	bl.logger.Info().Msgf("Starting from stored block %d.", resp.Number)

	return big.NewInt(resp.Number), nil
}

// RecordBlock store block number and hash after processing
func (bl *BlockListener) RecordBlock(head *types.Header) error {
	processedBlock := models.Block{
		ChainID: bl.chainID,
		Number:  head.Number.Int64(),
		Hash:    head.Hash().Bytes(),
	}
	return processedBlock.Upsert(context.Background(), bl.db.DBS().Writer, true, []string{"chain_id"}, boil.Whitelist("number", "hash", "processed_at"), boil.Infer())
}

func (bl *BlockListener) GetBlockHead(blockNum *big.Int) (*types.Header, error) {
	timer := prometheus.NewTimer(metrics.AlchemyHeadPollResponseTime)
	var head *types.Header
	err := retry(func() error {
		var err error
		head, err = bl.client.HeaderByNumber(context.Background(), blockNum)
		if err != nil {
			metrics.CountRetries.With(prometheus.Labels{"type": "HeaderByNumber"}).Inc()
		}
		return err
	}, MaxRetries, RetryDuration)
	if err != nil {
		metrics.FailedHeadPolls.Inc()
		return nil, err
	}
	metrics.CountRetries.With(prometheus.Labels{"type": "HeaderByNumber"}).Set(0)
	timer.ObserveDuration()
	metrics.SuccessfulHeadPolls.Inc()

	return head, err
}

func (bl *BlockListener) ProcessBlock(ctx context.Context, blockNum *big.Int) error {
	logger := bl.logger.With().Int64("chainId", bl.chainID).Int64("blockNumber", blockNum.Int64()).Logger()

	head, err := bl.GetBlockHead(blockNum)
	if err != nil {
		return fmt.Errorf("error getting block %d header: %w", blockNum, err)
	}

	logger.Info().Str("hash", head.Hash().Hex()).Msg("Processing block.")

	tm := time.Unix(int64(head.Time), 0)
	var logs []types.Log
	err = retry(func() error {
		var err error
		timer := prometheus.NewTimer(metrics.FilteredLogsResponseTime)
		logs, err = bl.client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: blockNum,
			ToBlock:   blockNum,
			Addresses: bl.contracts,
		})
		if err != nil {
			metrics.FailedFilteredLogsFetch.Inc()
			metrics.CountRetries.With(prometheus.Labels{"type": "FilterLogs"}).Inc()
		} else {
			timer.ObserveDuration()
		}
		return err
	}, MaxRetries, RetryDuration)
	if err != nil {
		return fmt.Errorf("failed retrieving log for block %d: %w", blockNum, err)
	}
	metrics.SuccessfulFilteredLogsFetch.Inc()
	metrics.CountRetries.With(prometheus.Labels{"type": "FilterLogs"}).Set(0)

	for _, vLog := range logs {
		logger := logger.With().Uint("index", vLog.Index).Str("contract", vLog.Address.Hex()).Logger()

		if vLog.Removed {
			logger.Warn().Msg("Log removed after reorganization. This should never happen.")
		}

		contractABI, ok := bl.ABIs[vLog.Address]
		if !ok {
			logger.Warn().Msgf("Unrecognized contract %s.", vLog.Address)
			continue
		}

		fromMetaTx := false

		if bl.registryAddress != nil && len(bl.registryAddress) != 0 && vLog.Address == *bl.registryAddress {
			tx, _, err := bl.client.TransactionByHash(ctx, vLog.TxHash)
			if err != nil {
				return fmt.Errorf("failed retrieving registry transaction %q: %w", vLog.TxHash, err)
			}

			sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(bl.chainID)), tx)
			if err != nil {
				return fmt.Errorf("couldn't convert transaction %q to message: %w", vLog.TxHash, err)
			}

			if slices.Contains(bl.relayAddresses, sender) {
				fromMetaTx = true
			}
		}

		eventDef, err := contractABI.EventByID(vLog.Topics[0])
		if err != nil {
			logger.Warn().Err(err).Msgf("Unrecognized event signature %s.", vLog.Topics[0])
			continue
		}

		var indexed abi.Arguments
		for _, arg := range eventDef.Inputs {
			if arg.Indexed {
				indexed = append(indexed, arg)
			}
		}

		args := map[string]any{}

		// Parse the non-indexed fields.
		if err := bl.ABIs[vLog.Address].UnpackIntoMap(args, eventDef.Name, vLog.Data); err != nil {
			logger.Err(err).Msg("Failed to parse log data.")
			continue
		}

		// Parse the indexed fields.
		err = abi.ParseTopicsIntoMap(args, indexed, vLog.Topics[1:])
		if err != nil {
			logger.Err(err).Msg("Failed to parse topics.")
			continue
		}

		logger.Info().Msg("Emitting event.")

		event := shared.CloudEvent[Event]{
			ID:          ksuid.New().String(),
			Source:      fmt.Sprintf("chain/%d", bl.chainID),
			Subject:     hexutil.Encode(vLog.Address.Bytes()),
			Type:        "zone.dimo.contract.event",
			Time:        tm,
			SpecVersion: "1.0",
			Data: Event{
				EventName: eventDef.Name,
				Block: Block{
					Number: head.Number,
					Hash:   head.Hash(),
					Time:   tm,
				},
				Index:               vLog.Index,
				Contract:            vLog.Address,
				TransactionHash:     vLog.TxHash,
				EventSignature:      vLog.Topics[0],
				Arguments:           args,
				ChainID:             bl.chainID,
				FromMetaTransaction: fromMetaTx,
			}}

		eBytes, err := json.Marshal(event)
		if err != nil {
			logger.Err(err).Msg("Failed to marshal event.")
			continue
		}

		message := &sarama.ProducerMessage{Topic: bl.eventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
		_, _, err = bl.producer.SendMessage(message)
		if err != nil {
			logger.Err(err).Msg("Failed to emit event.")
			metrics.KafkaEventMessageFailedToSend.Inc()
			continue
		}

		metrics.EventsEmitted.Inc()
		metrics.KafkaEventMessageSent.Inc()
	}

	event := shared.CloudEvent[Block]{
		ID:     ksuid.New().String(),
		Source: fmt.Sprintf("chain/%d", bl.chainID),
		Type:   "zone.dimo.block.complete",
		Time:   tm,
		Data: Block{
			Number: head.Number,
			Hash:   head.Hash(),
			Time:   tm,
		},
	}

	eBytes, _ := json.Marshal(event)
	message := &sarama.ProducerMessage{Topic: bl.eventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
	_, _, err = bl.producer.SendMessage(message)
	if err != nil {
		logger.Info().Int64("chain", bl.chainID).Str("Block", head.Number.String()).Msgf("error sending block completion confirmation: %v", err)
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

func (bl *BlockListener) ProcessBlocks(ctx context.Context, ch <-chan *big.Int) error {
	bl.logger.Info().Msg("chain indexer starting")

	for {
		select {
		case block := <-ch:
			bl.Limit--
			// Need to figure out how to retry and eventually kill the pod.
			err := bl.ProcessBlock(ctx, block)
			if err != nil {
				bl.logger.Err(err).Msg("error processing blocks")
			}

			if bl.DevTest && bl.Limit < 0 {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}
