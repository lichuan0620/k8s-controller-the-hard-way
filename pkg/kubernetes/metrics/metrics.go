package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// InstallDefault install all available metrics to the default Prometheus registerer
func InstallDefault() {
	workqueue.SetProvider(BuildWorkqueueMetricsProvider(nil, prometheus.DefaultRegisterer))
	cache.SetReflectorMetricsProvider(BuildReflectorMetricsProvider(nil, prometheus.DefaultRegisterer))
}
