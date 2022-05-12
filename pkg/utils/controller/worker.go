package controller

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// Worker defines a worker that is managed by the controller and is responsible for calling
// the reconcile function. It controls concurrency and the frequency of reconciliation.
// Most of the time, you want to use NewWorker to construct the default worker, but you can
// define your own implementation if you know what you are doing.
type Worker interface {
	Run(
		queue workqueue.RateLimitingInterface,
		reconcile func(context.Context, string) (Result, error),
		logger klog.Logger,
	)
}

// NewWorker constructs a new worker.
func NewWorker(timeout time.Duration, concurrency int, maxOps float64) Worker {
	ret := &worker{
		reconcileTimeout: timeout,
		concurrency:      concurrency,
	}
	if maxOps > 0 {
		ret.workerWaitPeriod = time.Duration(float64(time.Second) / maxOps)
	}
	return ret
}

type worker struct {
	concurrency      int
	reconcileTimeout time.Duration
	workerWaitPeriod time.Duration
}

func (w *worker) Run(
	queue workqueue.RateLimitingInterface,
	reconcile func(context.Context, string) (Result, error),
	logger klog.Logger,
) {
	// If concurrency is not a positive number, we simply wait until the queue is shutdown.
	// Usually it doesn't make sense to have non-positive concurrency, but it can be desirable
	// when we had multiple workers and we wanted to turn off some of them.
	if w.concurrency <= 0 {
		for !queue.ShuttingDown() {
			time.Sleep(time.Second)
		}
		return
	}
	var wg sync.WaitGroup
	wg.Add(w.concurrency)
	for i := 0; i < w.concurrency; i++ {
		go func() {
			defer wg.Done()
			stop := make(chan struct{})
			wait.NonSlidingUntil(func() {
				if !w.processItemFromQueue(queue, reconcile, logger) {
					close(stop)
				}
			}, w.workerWaitPeriod, stop)
		}()
	}
	wg.Wait()
}

func (w *worker) processItemFromQueue(
	queue workqueue.RateLimitingInterface,
	reconcile func(context.Context, string) (Result, error),
	logger klog.Logger,
) bool {
	item, shutdown := queue.Get()
	if shutdown {
		return false
	}
	logger.V(4).Info("item dequeued", "key", item.(string))
	defer queue.Done(item)
	ctx, cancel := context.Background(), func() {}
	if w.reconcileTimeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), w.reconcileTimeout)
	}
	defer cancel()
	if result, err := reconcile(ctx, item.(string)); err != nil {
		logger.Error(err, "reconciliation failure", "key", item.(string))
		queue.AddRateLimited(item)
	} else {
		logger.V(4).Info("reconciliation successful", "key", item.(string))
		queue.Forget(item)
		if result.Requeue || result.RequeueAfter > 0 {
			if result.RequeueAfter > 0 {
				queue.AddAfter(item, result.RequeueAfter)
			}
			queue.Add(item)
		}
	}
	return true
}
