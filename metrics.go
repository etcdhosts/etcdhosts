package etcdhosts

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// entriesTotal is a gauge that tracks the number of loaded host entries.
	entriesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "entries_total",
		Help:      "The total number of host entries currently loaded.",
	})

	// queriesTotal is a counter that tracks DNS queries with result labels.
	queriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "queries_total",
		Help:      "The total number of DNS queries processed.",
	}, []string{"qtype", "result"})

	// queryDuration is a histogram that tracks query processing duration.
	queryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "query_duration_seconds",
		Help:      "Histogram of query processing duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"qtype"})

	// etcdSyncTotal is a counter that tracks etcd sync operations.
	etcdSyncTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "etcd_sync_total",
		Help:      "The total number of etcd sync operations.",
	}, []string{"status"})

	// etcdLastSync is a gauge that tracks the timestamp of the last successful etcd sync.
	etcdLastSync = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "etcd_last_sync_timestamp_seconds",
		Help:      "Unix timestamp of the last successful etcd sync.",
	})

	// healthcheckStatus is a gauge that tracks health check status per target.
	healthcheckStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "healthcheck_status",
		Help:      "Health check status (1 = healthy, 0 = unhealthy).",
	}, []string{"hostname", "ip"})
)

// Result labels for queriesTotal metric
const (
	resultHit  = "hit"
	resultMiss = "miss"
)

// Status labels for etcdSyncTotal metric
const (
	statusSuccess = "success"
	statusError   = "error"
)
