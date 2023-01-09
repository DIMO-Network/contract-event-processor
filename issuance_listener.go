package main

import (
	"context"
	"encoding/json"
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
)

func NewIssuanceContractListener(listener BlockListener, producer sarama.SyncProducer, logger *zerolog.Logger) (IssuanceContractListener, error) {
	return IssuanceContractListener{
		Address:  common.HexToAddress("0x8129f3cD3EBA82136Caf5aB87E2321c958Da5B63"),
		Producer: producer,
		Logger:   logger,
		Listener: listener,
	}, nil

}

type IssuanceContractListener struct {
	Logger   *zerolog.Logger
	Address  common.Address
	Listener BlockListener
	Producer sarama.SyncProducer
}

func (rcl IssuanceContractListener) NextBlock() (*types.Header, error) {

	// pull latest block from redis
	// if redis is empty, get the most recent block on chain
	// return rcl.Listener.Client.HeaderByNumber(context.Background(), nil)

	head, err := rcl.Listener.Client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		log.Fatal(err)
	}
	head.Number = big.NewInt(37850310)
	return head, nil
}

func (rcl IssuanceContractListener) IssuanceContractIndexer() {

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Running...")

Loop:
	for {
		select {
		case <-tick.C:

			head, err := rcl.NextBlock()

			block, err := rcl.Listener.Client.HeaderByNumber(context.Background(), new(big.Int).Sub(head.Number, rcl.Listener.Confirmations))
			if err != nil {
				log.Fatal(err)
			}

			lastConf = &Block{Hash: block.Hash(), Number: block.Number}
			err = rcl.ProcessBlock(rcl.Listener.Client, lastConf)
			if err != nil {
				log.Fatal(err)
			}

			head.Number = new(big.Int).Add(head.Number, big.NewInt(1))

		case sig := <-sigChan:
			log.Printf("Received signal, terminating: %s", sig)
			break Loop
		}
	}

}

func (rcl IssuanceContractListener) ProcessBlock(client *ethclient.Client, block *Block) error {
	log.Printf("Processing block %s", block.Number)

	fil := ethereum.FilterQuery{
		BlockHash: &block.Hash,
		Addresses: []common.Address{rcl.Address},
	}
	logs, err := client.FilterLogs(context.Background(), fil)
	if err != nil {
		return err
	}

	for _, vLog := range logs {
		if vLog.Removed {
			rcl.Logger.Info().Uint64("Block Number", vLog.BlockNumber).Msg("Log removed")
		}

		fmt.Println("Block Number: ", vLog.BlockNumber)
		if ev, ok := rcl.Listener.Registry[rcl.Address][vLog.Topics[0]]; ok {

			event := shared.CloudEvent[Event]{
				ID:      ksuid.New().String(),
				Source:  string(vLog.Address.String()),
				Subject: vLog.TxHash.String(),
				Time:    time.Now().UTC(),
				Data: Event{
					Contract: vLog.Address.String(),
					Sig:      vLog.Topics[0].String(),
				}}

			event.Data.Arguments = make(map[string]any)
			err = ev.Inputs.UnpackIntoMap(event.Data.Arguments, vLog.Data)
			var indexed abi.Arguments
			for _, arg := range ev.Inputs {
				if arg.Indexed {
					indexed = append(indexed, arg)
				}
			}
			// TODO-- topic slice looks odd, was getting mismatched length error before
			err = abi.ParseTopicsIntoMap(event.Data.Arguments, indexed, vLog.Topics[len(vLog.Topics)-len(indexed):])
			if err != nil {
				log.Fatal(err)
			}

			eBytes, _ := json.Marshal(event)
			message := &sarama.ProducerMessage{Topic: rcl.Listener.EventStreamTopic, Key: sarama.StringEncoder(ksuid.New().String()), Value: sarama.ByteEncoder(eBytes)}
			_, _, err := rcl.Producer.SendMessage(message)
			if err != nil {
				rcl.Logger.Info().Str(rcl.Listener.EventStreamTopic, ev.Name).Msgf("error sending event to stream: %v", err)
			}

		}
	}

	return nil
}
