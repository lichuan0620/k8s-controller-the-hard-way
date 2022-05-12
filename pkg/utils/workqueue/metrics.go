package workqueue

import (
	"sync"
	"time"

	k8workqueue "k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
)

var (
	metricProvider        k8workqueue.MetricsProvider = &noopMetricsProvider{}
	metricProviderSetOnce sync.Once
)

// SetProvider sets the metrics provider for all subsequently created work queues. Only
// the first call has an effect.
func SetProvider(provider k8workqueue.MetricsProvider) {
	metricProviderSetOnce.Do(func() {
		metricProvider = provider
	})
}

func newMetrics(name string, clock clock.Clock) metrics {
	if len(name) == 0 || metricProvider == (noopMetricsProvider{}) {
		return noMetrics{}
	}
	return &defaultMetrics{
		clock:                   clock,
		depth:                   metricProvider.NewDepthMetric(name),
		adds:                    metricProvider.NewAddsMetric(name),
		latency:                 metricProvider.NewLatencyMetric(name),
		workDuration:            metricProvider.NewWorkDurationMetric(name),
		unfinishedWorkSeconds:   metricProvider.NewUnfinishedWorkSecondsMetric(name),
		longestRunningProcessor: metricProvider.NewLongestRunningProcessorSecondsMetric(name),
		addTimes:                map[t]time.Time{},
		processingStartTimes:    map[t]time.Time{},
	}
}

type metrics interface {
	add(item t)
	get(item t)
	done(item t)
	updateUnfinishedWork()
	retry()
}

type noMetrics struct{}

func (noMetrics) add(_ t)               {}
func (noMetrics) get(_ t)               {}
func (noMetrics) done(_ t)              {}
func (noMetrics) updateUnfinishedWork() {}
func (noMetrics) retry()                {}

type noopMetricsProvider struct{}

func (noopMetricsProvider) NewDepthMetric(_ string) k8workqueue.GaugeMetric {
	return noopMetric{}
}

func (noopMetricsProvider) NewAddsMetric(_ string) k8workqueue.CounterMetric {
	return noopMetric{}
}

func (noopMetricsProvider) NewLatencyMetric(_ string) k8workqueue.HistogramMetric {
	return noopMetric{}
}

func (noopMetricsProvider) NewWorkDurationMetric(_ string) k8workqueue.HistogramMetric {
	return noopMetric{}
}

func (noopMetricsProvider) NewUnfinishedWorkSecondsMetric(_ string) k8workqueue.SettableGaugeMetric {
	return noopMetric{}
}

func (noopMetricsProvider) NewLongestRunningProcessorSecondsMetric(_ string) k8workqueue.SettableGaugeMetric {
	return noopMetric{}
}

func (noopMetricsProvider) NewRetriesMetric(_ string) k8workqueue.CounterMetric {
	return noopMetric{}
}

type noopMetric struct{}

func (noopMetric) Inc()            {}
func (noopMetric) Dec()            {}
func (noopMetric) Set(float64)     {}
func (noopMetric) Observe(float64) {}

// defaultMetrics expects the caller to lock before setting any metrics.
type defaultMetrics struct {
	clock clock.Clock

	// current depth of a workqueue
	depth k8workqueue.GaugeMetric
	// total number of adds handled by a workqueue
	adds k8workqueue.CounterMetric
	// how long an item stays in a workqueue
	latency k8workqueue.HistogramMetric
	// how long processing an item from a workqueue takes
	workDuration         k8workqueue.HistogramMetric
	addTimes             map[t]time.Time
	processingStartTimes map[t]time.Time

	// how long have current threads been working?
	unfinishedWorkSeconds   k8workqueue.SettableGaugeMetric
	longestRunningProcessor k8workqueue.SettableGaugeMetric

	// number of retries, effectively counting function calls to AddAfter and AddRateLimited
	retries k8workqueue.CounterMetric
}

func (m *defaultMetrics) add(item t) {
	if m == nil {
		return
	}

	m.adds.Inc()
	m.depth.Inc()
	if _, exists := m.addTimes[item]; !exists {
		m.addTimes[item] = m.clock.Now()
	}
}

func (m *defaultMetrics) get(item t) {
	if m == nil {
		return
	}

	m.depth.Dec()
	m.processingStartTimes[item] = m.clock.Now()
	if startTime, exists := m.addTimes[item]; exists {
		m.latency.Observe(m.sinceInSeconds(startTime))
		delete(m.addTimes, item)
	}
}

func (m *defaultMetrics) done(item t) {
	if m == nil {
		return
	}

	if startTime, exists := m.processingStartTimes[item]; exists {
		m.workDuration.Observe(m.sinceInSeconds(startTime))
		delete(m.processingStartTimes, item)
	}
}

func (m *defaultMetrics) updateUnfinishedWork() {
	// Note that a summary metric would be better for this, but prometheus
	// doesn't seem to have non-hacky ways to reset the summary metrics.
	var total float64
	var oldest float64
	for _, t := range m.processingStartTimes {
		age := m.sinceInSeconds(t)
		total += age
		if age > oldest {
			oldest = age
		}
	}
	m.unfinishedWorkSeconds.Set(total)
	m.longestRunningProcessor.Set(oldest)
}

// Gets the time since the specified start in seconds.
func (m *defaultMetrics) sinceInSeconds(start time.Time) float64 {
	return m.clock.Since(start).Seconds()
}

func (m *defaultMetrics) retry() {
	if m == nil {
		return
	}

	m.retries.Inc()
}
