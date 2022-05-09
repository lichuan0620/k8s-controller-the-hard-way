package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"
)

const reflectorSubsystem = "reflector"

// BuildReflectorMetricsProvider construct a cache.MetricProvider and register it to the given
// Prometheus registerer, but does not call cache.SetProvider.
func BuildReflectorMetricsProvider(constLabel map[string]string, registerer prometheus.Registerer) cache.MetricsProvider {
	reflectorNameLabel := []string{"name"}

	ret := reflectorMetricsProvider{
		reflectorListsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "lists_total",
				Help:        "The number of list API requests that a reflector have done",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorListsDuration: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "list_duration_seconds",
				Help:        "The amount of time it takes for a reflector to receive and decode listed items",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorItemsPerList: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "items_per_list",
				Help:        "The number of items received by a reflector from a list API request",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorWatchesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "watches_total",
				Help:        "The number of API watches that a reflector have done",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorShortWatchesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "short_watches_total",
				Help:        "The number of short API watches that a reflector have done",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorWatchDuration: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "watch_duration_seconds",
				Help:        "The amount of time it takes for a reflector to receive and decode watched items",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorItemsPerWatch: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "items_per_watch",
				Help:        "The number of items received by a reflector from a watch result",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
		reflectorLastResourceVersion: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Subsystem:   reflectorSubsystem,
				Name:        "last_resource_version",
				Help:        "The last resource version observed by a reflector",
				ConstLabels: constLabel,
			},
			reflectorNameLabel,
		),
	}

	registerer.MustRegister(
		ret.reflectorListsTotal,
		ret.reflectorListsDuration,
		ret.reflectorItemsPerList,
		ret.reflectorWatchesTotal,
		ret.reflectorShortWatchesTotal,
		ret.reflectorWatchDuration,
		ret.reflectorItemsPerWatch,
		ret.reflectorLastResourceVersion,
	)

	return &ret
}

type reflectorMetricsProvider struct {
	reflectorListsTotal          *prometheus.CounterVec
	reflectorListsDuration       *prometheus.SummaryVec
	reflectorItemsPerList        *prometheus.SummaryVec
	reflectorWatchesTotal        *prometheus.CounterVec
	reflectorShortWatchesTotal   *prometheus.CounterVec
	reflectorWatchDuration       *prometheus.SummaryVec
	reflectorItemsPerWatch       *prometheus.SummaryVec
	reflectorLastResourceVersion *prometheus.GaugeVec
}

func (rmp *reflectorMetricsProvider) NewListsMetric(name string) cache.CounterMetric {
	return rmp.reflectorListsTotal.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewListDurationMetric(name string) cache.SummaryMetric {
	return rmp.reflectorListsDuration.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewItemsInListMetric(name string) cache.SummaryMetric {
	return rmp.reflectorItemsPerList.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewWatchesMetric(name string) cache.CounterMetric {
	return rmp.reflectorWatchesTotal.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewShortWatchesMetric(name string) cache.CounterMetric {
	return rmp.reflectorShortWatchesTotal.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewWatchDurationMetric(name string) cache.SummaryMetric {
	return rmp.reflectorWatchDuration.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewItemsInWatchMetric(name string) cache.SummaryMetric {
	return rmp.reflectorItemsPerWatch.WithLabelValues(name)
}

func (rmp *reflectorMetricsProvider) NewLastResourceVersionMetric(name string) cache.GaugeMetric {
	return rmp.reflectorLastResourceVersion.WithLabelValues(name)
}
