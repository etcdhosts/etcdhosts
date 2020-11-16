package etcdhosts

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// hostsEntries is the combined number of entries in hosts and Corefile.
	hostsEntries = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "etcdhosts",
		Name:      "entries",
		Help:      "The combined number of entries in hosts and Corefile.",
	}, []string{})
)
