package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/DIMO-Network/contract-event-processor/internal/services"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", "event-stream-processor").Logger()
	settings, err := shared.LoadConfig[config.Settings]("settings.yaml")
	if err != nil {
		logger.Fatal().Err(err)
	}

	if len(settings.Chains) < 1 {
		logger.Fatal().Err(errors.New("at least one valid chain ID must be passed"))
	}

	limit := flag.Int("limit", -1, "limit number of block iterations during development")
	flag.Parse()

	if len(os.Args) > 1 {
		switch subCommand := os.Args[1]; subCommand {
		case "migrate":
			command := "up"
			if len(os.Args) > 2 {
				command = os.Args[2]
				if command == "down-to" || command == "up-to" {
					command = command + " " + os.Args[3]
				}
			}
			migrateDatabase(logger, &settings, command)
			return
		}
	}

	monApp := serveMonitoring(settings.MonitoringPort, &logger)

	kafkaClient, err := services.StartKafkaStream(settings)
	if err != nil {
		log.Fatal(err)
	}
	defer kafkaClient.Close()

	producer, err := sarama.NewSyncProducerFromClient(kafkaClient)
	if err != nil {
		log.Fatal(err)
	}

	var shutdownSignal uint64
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	for _, chain := range settings.Chains {
		listener, err := services.NewBlockListener(settings, logger, producer, fmt.Sprintf("config-%s.yaml", settings.Environment), chain)
		if err != nil {
			log.Fatal(err)
		}

		listener.Limit = *limit
		if listener.Limit > 0 {
			listener.DevTest = true
		}

		go listener.ChainIndexer(listener.StartBlock, &shutdownSignal)
	}

	select {
	case sig := <-sigChan:
		logger.Info().Msgf("Received signal, terminating: %s", sig)
		atomic.AddUint64(&shutdownSignal, 1)

	}

	// TODO(elffjs): Log this.
	_ = monApp.Shutdown()
}

func serveMonitoring(port string, logger *zerolog.Logger) *fiber.App {
	monApp := fiber.New(fiber.Config{DisableStartupMessage: true})

	// Health check.
	monApp.Get("/", func(c *fiber.Ctx) error { return nil })
	monApp.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	go func() {
		if err := monApp.Listen(":" + port); err != nil {
			logger.Fatal().Err(err).Str("port", port).Msg("Failed to start monitoring web server.")
		}
	}()

	logger.Info().Str("port", port).Msg("Started monitoring web server.")

	return monApp
}
