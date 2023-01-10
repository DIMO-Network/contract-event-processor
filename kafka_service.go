package main

import (
	"github.com/Shopify/sarama"
)

type Event struct {
	Contract  string
	Sig       string
	Arguments map[string]any
}

func startKafkaStream(s Settings) (sarama.Client, error) {

	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Producer.Partitioner = sarama.NewHashPartitioner

	admin, err := sarama.NewClusterAdmin([]string{s.KafkaBroker}, config)
	if err != nil {
		return nil, err
	}
	defer admin.Close()

	tpcs, err := admin.ListTopics()
	if err != nil {
		return nil, err
	}

	if _, ok := tpcs[s.EventStreamTopic]; !ok {
		err = admin.CreateTopic(s.EventStreamTopic, &sarama.TopicDetail{
			NumPartitions:     int32(s.Partitions),
			ReplicationFactor: 1,
		}, false)
		if err != nil {
			return nil, err
		}
	}

	return sarama.NewClient([]string{s.KafkaBroker}, config)
}