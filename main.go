package main

import (
	"log"
	"os"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", "event-stream-processor").Logger()
	settings, err := shared.LoadConfig[Settings]("settings.yaml")
	if err != nil {
		log.Fatal(err)
	}

	listener, err := NewBlockListener(settings)
	if err != nil {
		log.Fatal(err)
	}

	kafkaClient, err := startKafkaStream(settings)
	if err != nil {
		log.Fatal(err)
	}
	defer kafkaClient.Close()

	producer, err := sarama.NewSyncProducerFromClient(kafkaClient)
	if err != nil {
		log.Fatal(err)
	}

	rewardClient, err := NewRewardContractListener(listener, producer, &logger)
	randomContract, err := NewRandomContractListener(listener, producer, &logger)

	go rewardClient.RewardContractListener()
	// random contract here bc its more active than ours and is easier to test with
	randomContract.RandomContractListener()

}
