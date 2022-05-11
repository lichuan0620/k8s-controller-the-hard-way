// nolint:revive
package controllers

import (
	"context"
	"sync"
	"time"

	demov1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	clientset "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/clientset/versioned"
	demov1informers "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/informers/externalversions/demo/v1"
	demov1listers "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/listers/demo/v1"
	"github.com/pkg/errors"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type TenantController struct {
	demoClient clientset.Interface

	redisLister  demov1listers.RedisLister
	tenantLister demov1listers.TenantLister

	queue workqueue.RateLimitingInterface

	concurrency      int
	reconcileTimeout time.Duration
	logger           klog.Logger
}

func NewTenantController(
	demoClient clientset.Interface,
	redisLister demov1listers.RedisLister,
	tenantInformer demov1informers.TenantInformer,
	concurrency int,
	reconcileTimeout time.Duration,
) *TenantController {
	const moduleName = "tenant-controller"
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), moduleName)
	logger := klog.NewKlogr().WithName(moduleName)
	tenantInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				logger.Error(err, "failed to parse meta namespace key from Add event")
				return
			}
			queue.Add(key)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMeta, err := meta.Accessor(oldObj)
			if err != nil {
				logger.Error(err, "failed to parse old object metadata from Update event")
				return
			}
			newMeta, err := meta.Accessor(newObj)
			if err != nil {
				logger.Error(err, "failed to parse new object metadata from Update event")
				return
			}
			if oldMeta.GetResourceVersion() != newMeta.GetResourceVersion() {
				queue.Add(newMeta.GetName())
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				logger.Error(err, "failed to parse meta namespace key from Delete event")
				return
			}
			queue.Add(key)
		},
	})
	return &TenantController{
		demoClient:       demoClient,
		redisLister:      redisLister,
		tenantLister:     tenantInformer.Lister(),
		queue:            queue,
		concurrency:      concurrency,
		reconcileTimeout: reconcileTimeout,
		logger:           logger,
	}
}

func (c *TenantController) Run(ctx context.Context) {
	go func() {
		<-ctx.Done()
		c.queue.ShutDown()
	}()
	var wg sync.WaitGroup
	wg.Add(c.concurrency)
	for i := 0; i < c.concurrency; i++ {
		go func() {
			defer wg.Done()
			wait.Until(c.processItemFromQueue, time.Millisecond, ctx.Done())
		}()
	}
	wg.Wait()
}

func (c *TenantController) Enqueue(key string) {
	c.queue.Add(key)
}

func (c *TenantController) processItemFromQueue() {
	item, shutdown := c.queue.Get()
	if shutdown {
		return
	}
	defer c.queue.Done(item)
	ctx, cancel := context.Background(), func() {}
	if c.reconcileTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, c.reconcileTimeout)
	}
	defer cancel()
	key := item.(string)
	if err := c.reconcile(ctx, key); err != nil {
		c.logger.Error(err, "reconciliation failed", "key", key)
		c.queue.AddRateLimited(item)
	} else {
		c.logger.Info("reconciliation successful", "key", key)
		c.queue.Forget(item)
	}
}

func (c *TenantController) reconcile(ctx context.Context, key string) (err error) {
	tenant, err := c.tenantLister.Get(key)
	if err != nil {
		// if the object cannot be found, it probably has been deleted
		if k8errors.IsNotFound(err) {
			return nil
		}
		return errors.Wrap(err, "get Redis")
	}
	updated := false
	defer func() {
		if updated {
			_, updatedErr := c.demoClient.DemoV1().Tenants().Update(ctx, tenant, metav1.UpdateOptions{})
			if updatedErr != nil && err == nil {
				err = updatedErr
			}
		}
	}()
	list, err := c.redisLister.List(labels.SelectorFromSet(map[string]string{demov1.LabelTenant: key}))
	if err != nil {
		return errors.Wrap(err, "list Redis")
	}
	ownedRedisCount := len(list)
	containFinalizer := func() bool {
		for _, finalizer := range tenant.Finalizers {
			if finalizer == demov1.FinalizerOwnedResource {
				return true
			}
		}
		return false
	}()
	if ownedRedisCount > 0 && !containFinalizer {
		tenant.Finalizers = append(tenant.Finalizers, demov1.FinalizerOwnedResource)
		updated = true
	} else if ownedRedisCount == 0 && containFinalizer {
		var newFinalizer []string
		for _, finalizer := range tenant.Finalizers {
			if finalizer != demov1.FinalizerOwnedResource {
				newFinalizer = append(newFinalizer, finalizer)
			}
		}
		tenant.Finalizers = newFinalizer
		updated = true
	}

	if tenant.DeletionTimestamp != nil && !tenant.DeletionTimestamp.IsZero() {
		if containFinalizer {
			c.queue.AddAfter(key, 3*time.Second)
		}
		return nil
	}
	if ownedRedisCount != tenant.OwnedRedisCount {
		tenant.OwnedRedisCount = ownedRedisCount
		updated = true
	}
	return nil
}
