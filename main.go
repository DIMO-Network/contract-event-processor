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
	listener.CompileRegistryMap("config.yaml")

	kafkaClient, err := startKafkaStream(settings)
	if err != nil {
		log.Fatal(err)
	}
	defer kafkaClient.Close()

	producer, err := sarama.NewSyncProducerFromClient(kafkaClient)
	if err != nil {
		log.Fatal(err)
	}

	issuanceClient, err := NewIssuanceContractListener(listener, producer, &logger)

	issuanceClient.IssuanceContractIndexer()

}
