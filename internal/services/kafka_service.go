package services

import (
	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/Shopify/sarama"
)

func StartKafkaStream(s config.Settings) (sarama.Client, error) {

	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.Partitioner = sarama.NewHashPartitioner
	return sarama.NewClient([]string{s.KafkaBrokers}, config)
}
