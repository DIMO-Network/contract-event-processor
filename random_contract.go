package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
)

func NewRandomContractListener(listener BlockListener, producer sarama.SyncProducer, logger *zerolog.Logger) (RandomContractListener, error) {

	return RandomContractListener{
		Address:          listener.RandomContract,
		Client:           listener.Client,
		EventStreamTopic: listener.EventStreamTopic,
		Producer:         producer,
		Logger:           logger,
	}, nil

}

func (rcl RandomContractListener) RandomContractListener() {
	fmt.Println("\tListening To: ", rcl.Address)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{common.HexToAddress(rcl.Address)},
	}

	logs := make(chan types.Log)

	sub, err := rcl.Client.SubscribeFilterLogs(context.Background(), query, logs)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Running...")
	for {
		select {
		case err := <-sub.Err():
			log.Fatal(err, 3)
		case vLog := <-logs:
			v := shared.CloudEvent[Event]{
				ID:     ksuid.New().String(),
				Source: string(vLog.Address.String()),
				Data:   Event{TxHash: vLog.TxHash.String()},
			}
			b, _ := json.Marshal(v)
			message := &sarama.ProducerMessage{Topic: rcl.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.StringEncoder(string(b))}
			_, _, err = rcl.Producer.SendMessage(message)
			if err != nil {
				rcl.Logger.Info().Str(rcl.EventStreamTopic, vLog.Topics[0].String()).Msgf("error sending event to stream: %v", err)
			}

		}
	}

}
