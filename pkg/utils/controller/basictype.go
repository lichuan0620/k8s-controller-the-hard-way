package controller

import (
	"context"
	"time"

	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client"
	"github.com/pkg/errors"
	kubecache "k8s.io/client-go/tools/cache"
	k8workqueue "k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// BasicTypeOptions is the extended Options for basic controller.
type BasicTypeOptions struct {
	Options

	// ReconcileTimeout set the timeout for reconciliation.
	ReconcileTimeout time.Duration
	// Concurrency is the number of parallel subroutines for the worker.
	Concurrency int
}

// NewBasicController builds a basic controller with a single worker.
func NewBasicController(options BasicTypeOptions) (Controller, error) {
	if err := validateOptions(&options.Options); err != nil {
		return nil, err
	}
	gvk, err := apiutil.GVKForObject(options.Resource, client.Scheme)
	if err != nil {
		return nil, errors.Wrap(err, "parse GroupVersionKind")
	}
	gk := gvk.GroupKind().String()
	logger := klog.NewKlogr().WithName(options.GetName()).WithValues("resource", gk)
	queue := k8workqueue.NewNamedRateLimitingQueue(k8workqueue.DefaultControllerRateLimiter(), options.GetName())
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
	return &basicType{
		reconciler:       options.Reconciler,
		queue:            queue,
		waitForCacheSync: options.Informers.WaitForCacheSync,
		worker:           NewWorker(options.ReconcileTimeout, options.Concurrency, options.MaxWorkerOPS),
		logger:           logger,
	}, nil
}

type basicType struct {
	reconciler       Reconciler
	queue            k8workqueue.RateLimitingInterface
	waitForCacheSync func(ctx context.Context) bool
	worker           Worker
	logger           klog.Logger
}

func (t *basicType) Run(ctx context.Context) {
	t.logger.V(4).Info("waiting for the informers to sync")
	if !t.waitForCacheSync(ctx) {
		t.logger.Info("informers cannot sync")
		return
	}
	t.logger.Info("running")
	go func() {
		t.logger.V(4).Info("stopping")
		<-ctx.Done()
		t.queue.ShutDown()
	}()
	t.worker.Run(t.queue, t.reconciler.Reconcile, t.logger)
	t.logger.Info("stopped")
}

func (t *basicType) Enqueue(key string) {
	t.queue.Add(key)
}

func (t *basicType) EnqueueObject(obj runtimeclient.Object) {
	t.queue.Add(runtimeclient.ObjectKeyFromObject(obj))
}
