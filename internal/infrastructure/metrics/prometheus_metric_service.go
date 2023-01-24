package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	EventsEmitted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "events_emitted_total",
			Help: "get the total number of events emitted across all contracts",
		},
	)

	BlocksProcessed = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "processed_blocks_total",
			Help: "total number of processed blocks since start up",
		},
	)

	BlocksInQueue = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "queued_blocks_count",
			Help: "number of blocks currently in queue that have not yet been processed",
		},
	)

	ProcessedBlockNumberStored = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "processed_blocks_storage_success",
			Help: "total number of blocks that have been successfully processed and recorded in db",
		},
	)

	ProcessedBlockNumberStoreFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "processed_blocks_storage_failed",
			Help: "get the total number of events emitted across all contracts",
		},
	)

	KafkaEventMessageSent = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kafka_event_message_sent",
			Help: "total number of successful kafka event messages sent since start up",
		},
	)

	KafkaEventMessageFailedToSend = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "kafka_event_message_failed",
			Help: "total number of kafka event messages that failed to send since start up",
		},
	)

	SuccessfulHeadPolls = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "head_poll_successful",
			Help: "total number of successful block head polls sicne start up",
		},
	)

	FailedHeadPolls = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "head_poll_failed",
			Help: "total number of failed block head polls sicne start up",
		},
	)

	AlchemyHeadPollResponseTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "alchemy_head_poll_response_time",
			Help:    "average response time of alchemy head poll requests",
			Buckets: prometheus.DefBuckets,
		},
	)

	SuccessfulFilteredLogsFetch = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "filtered_log_success",
			Help: "total number of successful pulls of filtered block logs since start up",
		},
	)

	FailedFilteredLogsFetch = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "filtered_log_failed",
			Help: "total number of failed pulls of filtered block logs since start up",
		},
	)

	FilteredLogsResponseTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "filtered_log_response_time",
			Help:    "average response time of filtered log requests",
			Buckets: prometheus.DefBuckets,
		},
	)
)
