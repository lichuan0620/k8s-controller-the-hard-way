/*
k8s-controller-the-hard-way, a guide to really, really understand how to program k8s controllers
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	"context"
	time "time"

	demov1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	versioned "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/clientset/versioned"
	internalinterfaces "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/informers/externalversions/internalinterfaces"
	v1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/listers/demo/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// TenantInformer provides access to a shared informer and lister for
// Tenants.
type TenantInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.TenantLister
}

type tenantInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewTenantInformer constructs a new informer for Tenant type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewTenantInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredTenantInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredTenantInformer constructs a new informer for Tenant type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredTenantInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.DemoV1().Tenants().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.DemoV1().Tenants().Watch(context.TODO(), options)
			},
		},
		&demov1.Tenant{},
		resyncPeriod,
		indexers,
	)
}

func (f *tenantInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredTenantInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *tenantInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&demov1.Tenant{}, f.defaultInformer)
}

func (f *tenantInformer) Lister() v1.TenantLister {
	return v1.NewTenantLister(f.Informer().GetIndexer())
}