package main

import (
	"log"
	"math/big"
	"os"
	"strconv"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", "event-stream-processor").Logger()
	settings, err := shared.LoadConfig[Settings]("settings.yaml")
	if err != nil {
		logger.Fatal().Err(err)
	}

	var blockNum *big.Int
	if len(os.Args) > 1 {
		switch subCommand := os.Args[1]; subCommand {
		case "migrate":
			command := "up"
			migrateDatabase(logger, &settings, command, "chain_indexer")
			return
		case "override":
			if len(os.Args) > 2 {
				n, err := strconv.Atoi(os.Args[2])
				if err != nil {
					logger.Fatal().Err(err)
				}
				blockNum = big.NewInt(int64(n))

			}
		}
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

	listener, err := NewBlockListener(settings, logger, producer)
	if err != nil {
		log.Fatal(err)
	}

	listener.CompileRegistryMap("config.yaml")
	listener.ChainIndexer(blockNum)
}
