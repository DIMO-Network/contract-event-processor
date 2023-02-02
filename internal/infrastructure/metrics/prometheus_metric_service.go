package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	EventsEmitted = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "events_emitted_total",
			Help:      "get the total number of events emitted across all contracts",
		},
	)

	BlocksProcessed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "processed_blocks_total",
			Help:      "total number of processed blocks since start up",
		},
	)

	BlocksInQueue = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "contract_event_processor",
			Name:      "queued_blocks_count",
			Help:      "number of blocks currently in queue that have not yet been processed",
		},
	)

	ProcessedBlockNumberStored = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "processed_blocks_storage_success",
			Help:      "total number of blocks that have been successfully processed and recorded in db",
		},
	)

	ProcessedBlockNumberStoreFailed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "processed_blocks_storage_failed",
			Help:      "get the total number of events emitted across all contracts",
		},
	)

	KafkaEventMessageSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "kafka_event_message_sent",
			Help:      "total number of successful kafka event messages sent since start up",
		},
	)

	KafkaEventMessageFailedToSend = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "kafka_event_message_failed",
			Help:      "total number of kafka event messages that failed to send since start up",
		},
	)

	SuccessfulHeadPolls = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "head_poll_successful",
			Help:      "total number of successful block head polls sicne start up",
		},
	)

	FailedHeadPolls = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "head_poll_failed",
			Help:      "total number of failed block head polls sicne start up",
		},
	)

	AlchemyHeadPollResponseTime = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "contract_event_processor",
			Name:      "alchemy_head_poll_response_time",
			Help:      "average response time of alchemy head poll requests",
			Buckets:   prometheus.DefBuckets,
		},
	)

	SuccessfulFilteredLogsFetch = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "filtered_log_success",
			Help:      "total number of successful pulls of filtered block logs since start up",
		},
	)

	FailedFilteredLogsFetch = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "contract_event_processor",
			Name:      "filtered_log_failed",
			Help:      "total number of failed pulls of filtered block logs since start up",
		},
	)

	FilteredLogsResponseTime = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "contract_event_processor",
			Name:      "filtered_log_response_time",
			Help:      "average response time of filtered log requests",
			Buckets:   prometheus.DefBuckets,
		},
	)
)
