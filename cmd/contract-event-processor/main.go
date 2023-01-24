package main

import (
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"

	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/DIMO-Network/contract-event-processor/internal/infrastructure/metrics"
	"github.com/DIMO-Network/contract-event-processor/internal/services"

	"github.com/DIMO-Network/shared"
	"github.com/Shopify/sarama"
	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", "event-stream-processor").Logger()
	settings, err := shared.LoadConfig[config.Settings]("settings.yaml")
	if err != nil {
		logger.Fatal().Err(err)
	}

	var blockNum *big.Int
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

	listener, err := services.NewBlockListener(settings, logger, producer)
	if err != nil {
		log.Fatal(err)
	}

	startPrometheus(logger)
	listener.CompileRegistryMap("config.yaml")
	listener.ChainIndexer(blockNum)

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

func startPrometheus(logger zerolog.Logger) {

	prometheus.Register(metrics.EventsEmitted)
	prometheus.Register(metrics.BlocksProcessed)
	prometheus.Register(metrics.BlocksInQueue)
	prometheus.Register(metrics.ProcessedBlockNumberStored)
	prometheus.Register(metrics.ProcessedBlockNumberStoreFailed)
	prometheus.Register(metrics.KafkaEventMessageSent)
	prometheus.Register(metrics.KafkaEventMessageFailedToSend)
	prometheus.Register(metrics.SuccessfulHeadPolls)
	prometheus.Register(metrics.FailedHeadPolls)
	prometheus.Register(metrics.SuccessfulFilteredLogsFetch)
	prometheus.Register(metrics.FailedFilteredLogsFetch)
	prometheus.Register(metrics.FilteredLogsResponseTime)
	prometheus.Register(metrics.AlchemyHeadPollResponseTime)

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":8888", nil)
		if err != nil {
			logger.Fatal().Err(err).Msg("could not start consumer")
		}
	}()
	logger.Info().Msg("prometheus metrics at :8888/metrics")
}
