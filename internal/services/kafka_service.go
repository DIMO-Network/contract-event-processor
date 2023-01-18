package services

import (
	"github.com/DIMO-Network/contract-event-processor/internal/config"
	"github.com/Shopify/sarama"
)

type Event struct {
	Contract        string         `json:"contract,omitempty"`
	TransactionHash string         `json:"transactionHash,omitempty"`
	Arguments       map[string]any `json:"arguments,omitempty"`
	BlockCompleted  bool           `json:"blockCompleted,omitempty"`
	EventSignature  string         `json:"eventSignature,omitempty"`
	EventName       string         `json:"eventName,omitempty"`
}

func StartKafkaStream(s config.Settings) (sarama.Client, error) {

	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.Partitioner = sarama.NewHashPartitioner
	return sarama.NewClient([]string{s.KafkaBrokers}, config)
}
