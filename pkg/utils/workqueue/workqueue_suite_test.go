package workqueue

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	k8metrics "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

func TestWorkqueue(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "workqueue")
}

var _ = BeforeSuite(func() {
	SetProvider(k8metrics.BuildWorkqueueMetricsProvider(nil, prometheus.DefaultRegisterer))
})
