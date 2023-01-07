package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
)

func NewRewardContractListener(listener BlockListener, producer sarama.SyncProducer, logger *zerolog.Logger) (RewardContractListener, error) {
	abi, err := RewardMetaData.GetAbi()
	if err != nil {
		return RewardContractListener{}, err
	}

	return RewardContractListener{
		ABI:                    abi,
		Address:                listener.RewardContract,
		Client:                 listener.Client,
		EventStreamTopic:       listener.EventStreamTopic,
		Producer:               producer,
		Logger:                 logger,
		AdminChangedEvent:      abi.Events["AdminChanged"],
		TokensTransferredEvent: abi.Events["TokensTransferred"],
		AdminWithdrawalEvent:   abi.Events["AdminWithdrawal"],
		RoleGrantedEvent:       abi.Events["RoleGranted"],
		RoleAdminChangedEvent:  abi.Events["RoleAdminChanged"],
		RoleRevokedEvent:       abi.Events["RoleRevoked"],
		UpgradedEvent:          abi.Events["Upgraded"],
		BeaconUpgradedEvent:    abi.Events["BeaconUpgraded"],
		DidntQualifyEvent:      abi.Events["DidntQualify"],
		InitializedEvent:       abi.Events["Initialized"],
	}, nil

}

type RewardContractListener struct {
	ABI                    *abi.ABI
	Logger                 *zerolog.Logger
	Events                 map[string]abi.Event
	Address                string
	Client                 *ethclient.Client
	EventStreamTopic       string
	Producer               sarama.SyncProducer
	AdminChangedEvent      abi.Event
	TokensTransferredEvent abi.Event
	AdminWithdrawalEvent   abi.Event
	RoleGrantedEvent       abi.Event
	RoleAdminChangedEvent  abi.Event
	RoleRevokedEvent       abi.Event
	UpgradedEvent          abi.Event
	BeaconUpgradedEvent    abi.Event
	DidntQualifyEvent      abi.Event
	InitializedEvent       abi.Event
}

type RandomContractListener struct {
	Logger           *zerolog.Logger
	Address          string
	Client           *ethclient.Client
	Producer         sarama.SyncProducer
	EventStreamTopic string
}

type Event struct {
	TxHash string `json:"txHash"`
}

func (rcl RewardContractListener) RewardContractListener() {
	fmt.Println("\tListening To: ", rcl.Address)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{common.HexToAddress(rcl.Address)},
	}

	logs := make(chan types.Log)

	sub, err := rcl.Client.SubscribeFilterLogs(context.Background(), query, logs)
	if err != nil {
		rcl.Logger.Fatal().Msg(err.Error())
	}

	fmt.Println("Running...")
	for {
		select {
		case err := <-sub.Err():
			rcl.Logger.Fatal().Msg(err.Error())
		case vLog := <-logs:

			event := shared.CloudEvent[Event]{
				ID:     ksuid.New().String(),
				Source: string(vLog.Address.String()),
				Data:   Event{TxHash: vLog.TxHash.String()},
			}
			eBytes, _ := json.Marshal(event)

			switch vLog.Topics[0] {
			case rcl.AdminChangedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.AdminChangedEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.TokensTransferredEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.TokensTransferredEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.AdminWithdrawalEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.AdminWithdrawalEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.RoleGrantedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.RoleGrantedEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.RoleAdminChangedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.RoleAdminChangedEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.RoleRevokedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.RoleRevokedEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.UpgradedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.UpgradedEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.BeaconUpgradedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.BeaconUpgradedEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.DidntQualifyEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.DidntQualifyEvent.Name).Msgf("error sending event to stream: %v", err)
				}

			case rcl.InitializedEvent.ID:
				message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(eBytes))}
				_, _, err := rcl.Producer.SendMessage(message)
				if err != nil {
					rcl.Logger.Info().Str(rcl.EventStreamTopic, rcl.InitializedEvent.Name).Msgf("error sending event to stream: %v", err)
				}
			}

		}
	}

}
