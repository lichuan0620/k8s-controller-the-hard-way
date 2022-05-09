package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/util/workqueue"
)

const workQueueSubsystem = "workqueue"

// BuildWorkqueueMetricsProvider construct a workqueue.MetricProvider and register it to the
// given Prometheus registerer, but does not call workqueue.SetProvider.
func BuildWorkqueueMetricsProvider(constLabels map[string]string, registerer prometheus.Registerer) workqueue.MetricsProvider {
	workQueueNameLabel := []string{"name"}

	ret := workqueueMetricProvider{
		workqueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "depth",
				Help:        "Current depth of a workqueue",
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
		workqueueAdds: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "adds_total",
				Help:        "Total number of events added to a workqueue for the first time",
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
		workqueueQueueDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "queue_latency_seconds",
				Help:        "How long in seconds an item stays in a workqueue before being requested",
				Buckets:     prometheus.ExponentialBuckets(10e-9, 10, 10),
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
		workqueueWorkDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "work_duration_seconds",
				Help:        "How long in seconds processing an item from a workqueue takes.",
				Buckets:     prometheus.ExponentialBuckets(10e-9, 10, 10),
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
		workqueueUnfinished: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "unfinished_work_seconds",
				Help:        "How many seconds of work has been done that is in progress and hasn't been observed by work_duration",
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
		workqueueLongestRun: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "longest_running_processor_seconds",
				Help:        "How many seconds has the longest running processor for a workqueue been running.",
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
		workqueueRetries: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem:   workQueueSubsystem,
				Name:        "retries_total",
				Help:        "Total number of events added to a workqueue due to retry",
				ConstLabels: constLabels,
			},
			workQueueNameLabel,
		),
	}

	registerer.MustRegister(
		ret.workqueueDepth,
		ret.workqueueAdds,
		ret.workqueueQueueDuration,
		ret.workqueueWorkDuration,
		ret.workqueueUnfinished,
		ret.workqueueLongestRun,
		ret.workqueueRetries,
	)

	return &ret
}

type workqueueMetricProvider struct {
	workqueueDepth         *prometheus.GaugeVec
	workqueueAdds          *prometheus.CounterVec
	workqueueQueueDuration *prometheus.HistogramVec
	workqueueWorkDuration  *prometheus.HistogramVec
	workqueueUnfinished    *prometheus.GaugeVec
	workqueueLongestRun    *prometheus.GaugeVec
	workqueueRetries       *prometheus.CounterVec
}

func (wmp *workqueueMetricProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return wmp.workqueueDepth.WithLabelValues(name)
}

func (wmp *workqueueMetricProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return wmp.workqueueAdds.WithLabelValues(name)
}

func (wmp *workqueueMetricProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	return wmp.workqueueQueueDuration.WithLabelValues(name)
}

func (wmp *workqueueMetricProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	return wmp.workqueueWorkDuration.WithLabelValues(name)
}

func (wmp *workqueueMetricProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return wmp.workqueueUnfinished.WithLabelValues(name)
}

func (wmp *workqueueMetricProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return wmp.workqueueLongestRun.WithLabelValues(name)
}

func (wmp *workqueueMetricProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return wmp.workqueueRetries.WithLabelValues(name)
}
