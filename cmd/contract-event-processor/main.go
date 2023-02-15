package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/DIMO-Network/contract-event-processor/internal/services"
	"golang.org/x/sync/errgroup"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", "contract-stream-processor").Logger()
	settings, err := shared.LoadConfig[config.Settings]("settings.yaml")
	if err != nil {
		logger.Fatal().Err(err).Msg("couldn't load settings")
	}

	if len(settings.Chains) < 1 {
		logger.Fatal().Err(errors.New("at least one valid chain ID must be passed"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	var group errgroup.Group

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

	group.Go(func() error { return serveMonitoring(ctx, settings.MonitoringPort, &logger) })

	kafkaClient, err := services.StartKafkaStream(settings)
	if err != nil {
		log.Fatal(err)
	}
	defer kafkaClient.Close()

	producer, err := sarama.NewSyncProducerFromClient(kafkaClient)
	if err != nil {
		log.Fatal(err)
	}

	for _, chain := range settings.Chains {
		listener, err := services.NewBlockListener(settings, logger, producer, fmt.Sprintf("config-%s.yaml", settings.Environment), chain)
		if err != nil {
			log.Fatal(err)
		}

		listener.Limit = *limit
		if listener.Limit > 0 {
			listener.DevTest = true
		}

		chainChan := make(chan *big.Int)
		group.Go(func() error { return listener.PollNewBlocks(ctx, listener.StartBlock, chainChan) })
		group.Go(func() error { return listener.ProcessBlocks(ctx, chainChan) })
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)
	<-sc
	cancel()
	_ = group.Wait()

}

func serveMonitoring(ctx context.Context, port string, logger *zerolog.Logger) error {
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
	<-ctx.Done()

	return monApp.Shutdown()
}
