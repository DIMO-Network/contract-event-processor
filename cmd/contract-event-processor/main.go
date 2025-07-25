package main

import (
	"context"
	"flag"
	"log"
	"math/big"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/DIMO-Network/contract-event-processor/internal/services"
	"golang.org/x/sync/errgroup"

	"github.com/DIMO-Network/shared"
	"github.com/IBM/sarama"
	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", "contract-event-processor").Logger()

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) == 40 {
				logger = logger.With().Str("commit", s.Value[:7]).Logger()
				break
			}
		}
	}

	settings, err := shared.LoadConfig[config.Settings]("settings.yaml")
	if err != nil {
		logger.Fatal().Err(err).Msg("couldn't load settings")
	}

	logger.Info().Str("env", settings.Environment).Int64("startingBlock", settings.StartingBlock).Msg("read settings")

	if settings.DIMORegistryAddress != "" && settings.RelayAddresses != "" {
		logger.Info().Msgf("DIMO registry address %s, relay addresses %s.", settings.DIMORegistryAddress, settings.RelayAddresses)
	}

	ctx, cancel := context.WithCancel(context.Background())
	group, ctx := errgroup.WithContext(ctx)

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

	listener, err := services.NewBlockListener(settings, logger, producer, "config.yaml")
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed creating block listener.")
	}

	listener.Limit = *limit
	if listener.Limit > 0 {
		listener.DevTest = true
	}

	chainChan := make(chan *big.Int)
	group.Go(func() error { return listener.PollNewBlocks(ctx, listener.StartBlock, chainChan) })
	group.Go(func() error { return listener.ProcessBlocks(ctx, chainChan) })

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
