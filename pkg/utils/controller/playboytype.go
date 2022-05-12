package controller

import (
	"context"
	"sync"
	"time"

	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/workqueue"
	"github.com/pkg/errors"
	kubecache "k8s.io/client-go/tools/cache"
	k8workqueue "k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// PlayboyTypeOptions configures a Controller of PlayboyType.
type PlayboyTypeOptions struct {
	Options

	// FreshThreshold is the number of time an item can be processed before it is considered staled.
	FreshThreshold int
	// FreshWorkerConcurrency is the number of parallel subroutines for the fresh worker. Fresh worker
	// processes items from the fresh queue.
	FreshWorkerConcurrency int
	// FreshWorkerTimeout is the reconciliation timeout for fresh workers.
	FreshWorkerTimeout time.Duration
	// FreshWorkerRetryDelay is the amount of time a fresh worker would wait before retrying a failed
	// item.
	FreshWorkerRetryDelay time.Duration
	// StaledWorkerConcurrency is the number of parallel subroutines for the staled worker. Staled worker
	// process items from the staled queue.
	StaledWorkerConcurrency int
	// StaledWorkerTimeout is the reconciliation timeout for staled workers.
	StaledWorkerTimeout time.Duration
	// StaledWorkerRetryDelay is the amount of time a staled worker would wait before retrying a failed
	// item.
	StaledWorkerRetryDelay time.Duration
}

type playboyType struct {
	reconciler       Reconciler
	queue            workqueue.PlayboyInterface
	waitForCacheSync func(ctx context.Context) bool
	freshWorker      Worker
	staledWorker     Worker
	logger           klog.Logger
}

// NewPlayboyType builds and returns a Controller that uses workqueue.PlayboyInterface internally.
// Two workers, fresh and staled, are constructed according to the given options. The fresh worker
// only get item from the fresh queue while the staled worker only from staled queue.
//
// If you are not sure how to use this, just use the default controller (by calling NewDefaultType).
func NewPlayboyType(options PlayboyTypeOptions) (Controller, error) {
	if err := validateOptions(&options.Options); err != nil {
		return nil, err
	}
	gvk, err := apiutil.GVKForObject(options.Resource, client.Scheme)
	if err != nil {
		return nil, errors.Wrap(err, "parse GroupVersionKind")
	}
	gk := gvk.GroupKind().String()
	logger := klog.NewKlogr().WithName(options.GetName()).WithValues("resource", gk)
	queue := workqueue.NewPlayboyQueue(
		options.FreshThreshold,
		k8workqueue.NewItemFastSlowRateLimiter(
			options.FreshWorkerRetryDelay,
			options.StaledWorkerRetryDelay,
			options.FreshThreshold,
		),
	)
	informer, err := options.Informers.GetInformer(context.TODO(), options.Resource)
	if err != nil {
		return nil, errors.Wrap(err, "get informer for "+gk)
	}
	handler := newEnqueueHandler(
		queue,
		kubecache.DeletionHandlingMetaNamespaceKeyFunc,
		options.Predicate,
		logger,
	)
	if options.ResyncPeriod == nil {
		informer.AddEventHandler(handler)
	} else {
		informer.AddEventHandlerWithResyncPeriod(handler, *options.ResyncPeriod)
	}
	for i := range options.WatchOptions {
		o := &options.WatchOptions[i]
		gvk, err = apiutil.GVKForObject(o.Resource, client.Scheme)
		if err != nil {
			return nil, errors.Wrap(err, "parse GroupVersionKind")
		}
		if informer, err = options.Informers.GetInformer(context.TODO(), o.Resource); err != nil {
			return nil, errors.Wrap(err, "get informer for "+gvk.GroupKind().String())
		}
		handler = newEnqueueHandler(
			queue,
			o.GetKeyFunc,
			o.Predicate,
			logger.WithValues("resource", gvk.GroupKind().String()),
		)
		if o.ResyncPeriod == nil {
			informer.AddEventHandler(handler)
		} else {
			informer.AddEventHandlerWithResyncPeriod(handler, *options.ResyncPeriod)
		}
	}
	return &playboyType{
		reconciler:       options.Reconciler,
		queue:            queue,
		waitForCacheSync: options.Informers.WaitForCacheSync,
		freshWorker:      NewWorker(options.FreshWorkerTimeout, options.FreshWorkerConcurrency, options.MaxWorkerOPS),
		staledWorker:     NewWorker(options.StaledWorkerTimeout, options.StaledWorkerConcurrency, options.MaxWorkerOPS),
		logger:           logger,
	}, nil
}

func (t *playboyType) Run(ctx context.Context) {
	t.logger.V(4).Info("waiting for the informers to sync")
	if !t.waitForCacheSync(ctx) {
		t.logger.Info("informers cannot sync")
		return
	}
	t.logger.Info("running")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		freshOnlyQueue := t.queue.FreshOnlyInterface()
		t.freshWorker.Run(
			freshOnlyQueue, t.reconciler.Reconcile, t.logger.WithValues("worker", "fresh"),
		)
	}()
	go func() {
		defer wg.Done()
		t.staledWorker.Run(
			t.queue, t.reconciler.Reconcile, t.logger.WithValues("worker", "staled"),
		)
	}()
	<-ctx.Done()
	t.logger.V(4).Info("stopping")
	t.queue.ShutDown()
	wg.Wait()
	t.logger.Info("stopped")
}

func (t *playboyType) Enqueue(key string) {
	t.queue.Add(key)
}

func (t *playboyType) EnqueueObject(obj runtimeclient.Object) {
	t.queue.Add(runtimeclient.ObjectKeyFromObject(obj))
}
