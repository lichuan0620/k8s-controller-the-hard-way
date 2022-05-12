// nolint:revive
package reconcilers

import (
	"context"
	"time"

	demov1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/controller"
	"github.com/pkg/errors"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func NewTenantReconciler(
	c runtimeclient.Client,
	reconcileTimeout time.Duration,
) controller.Reconciler {
	gvk, _ := apiutil.GVKForObject(&demov1.Redis{}, client.Scheme)
	gk := gvk.GroupKind().String()
	logger := klog.NewKlogr().WithName(gk).WithValues("resource", gk)
	return &tenantReconciler{
		client:           c,
		reconcileTimeout: reconcileTimeout,
		logger:           logger,
	}
}

type tenantReconciler struct {
	client           runtimeclient.Client
	reconcileTimeout time.Duration
	logger           klog.Logger
}

func (r *tenantReconciler) Reconcile(ctx context.Context, key string) (re controller.Result, err error) {
	var objectKey runtimeclient.ObjectKey
	objectKey.Namespace, objectKey.Name, err = cache.SplitMetaNamespaceKey(key)
	if err != nil {
		r.logger.Error(err, "failed to split meta namespace key; discarding invalid event")
		return controller.Result{}, nil
	}
	var tenant demov1.Tenant
	if err = r.client.Get(ctx, objectKey, &tenant); err != nil {
		// if the object cannot be found, it probably has been deleted
		if k8errors.IsNotFound(err) {
			return controller.Result{}, nil
		}
		return controller.Result{}, errors.Wrap(err, "get Tenant")
	}

	updated := false
	defer func() {
		if updated {
			err = errors.Wrap(r.client.Update(ctx, &tenant), "update Tenant")
		}
	}()

	var list demov1.RedisList
	if err = r.client.List(ctx, &list, &runtimeclient.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{demov1.LabelTenant: key}),
	}); err != nil {
		return controller.Result{}, errors.Wrap(err, "list Redis")
	}
	ownedRedisCount := len(list.Items)
	containsFinalizer := controllerutil.ContainsFinalizer(&tenant, demov1.FinalizerOwnedResource)
	if ownedRedisCount > 0 && !containsFinalizer {
		controllerutil.AddFinalizer(&tenant, demov1.FinalizerOwnedResource)
		updated = true
	} else if ownedRedisCount == 0 && containsFinalizer {
		controllerutil.RemoveFinalizer(&tenant, demov1.FinalizerOwnedResource)
		updated = true
	}

	if tenant.DeletionTimestamp != nil && !tenant.DeletionTimestamp.IsZero() {
		if containsFinalizer || ownedRedisCount > 0 {
			return controller.Result{RequeueAfter: 3 * time.Second}, nil
		}
		return controller.Result{}, nil
	}

	if ownedRedisCount != tenant.OwnedRedisCount {
		tenant.OwnedRedisCount = ownedRedisCount
		updated = true
	}
	return controller.Result{}, nil
}
