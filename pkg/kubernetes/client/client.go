package client

import (
	"time"

	demoscheme "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kuberest "k8s.io/client-go/rest"
	runtimecache "sigs.k8s.io/controller-runtime/pkg/cache"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Scheme is the runtime scheme used by all clients and informers. It combines the kubernetes schemes
// and our custom scheme.
var Scheme = runtime.NewScheme()

func init() {
	_ = demoscheme.AddToScheme(Scheme)
	_ = clientgoscheme.AddToScheme(Scheme)
}

// New creates a new delegating client.
// A delegating client forms a Client by composing separate reader, writer and statusclient
// interfaces. This way, you can have an Client that reads from a cache and writes to the
// API server.
func New(config *kuberest.Config, resync *time.Duration) (runtimeclient.Client, runtimecache.Informers, error) {
	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		return nil, nil, errors.Wrap(err, "construct mapper")
	}
	cache, err := runtimecache.New(config, runtimecache.Options{
		Scheme: Scheme,
		Mapper: mapper,
		Resync: resync,
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "construct controller-runtime cache")
	}
	client, err := runtimeclient.New(config, runtimeclient.Options{
		Scheme: Scheme,
		Mapper: mapper,
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "construct controller-runtime client")
	}
	dClient, err := runtimeclient.NewDelegatingClient(runtimeclient.NewDelegatingClientInput{
		CacheReader: cache,
		Client:      client,
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "construct controller-runtime delegating client")
	}
	return dClient, cache, nil
}

// NewDirect creates a client that read from the server instead of the cache.
func NewDirect(config *kuberest.Config) (runtimeclient.Client, error) {
	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		return nil, errors.Wrap(err, "construct mapper")
	}
	return runtimeclient.New(config, runtimeclient.Options{
		Scheme: Scheme,
		Mapper: mapper,
	})
}
