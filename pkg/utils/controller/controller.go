package controller

import (
	"context"
	"time"

	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client"
	"github.com/pkg/errors"
	kubecache "k8s.io/client-go/tools/cache"
	k8workqueue "k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	runtimecache "sigs.k8s.io/controller-runtime/pkg/cache"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Controller manages a workqueue and a Reconciler. It provides interfaces for enqueuing objects
// and concurrently pull these objects from the queue and use the Reconciler to handle them.
// In most cases, you should use the provided Controller implementation instead of implementing
// your own.
type Controller interface {
	// Run starts internal process of the controller.
	Run(context.Context)
	// Enqueue is used to add additional keys for the controller to process. This is for
	// special occasion only; in most cases, you want the resource event handler to do the
	// enqueueing for you.
	Enqueue(string)
	// EnqueueObject works like Enqueue, but takes a complete Object instead.
	EnqueueObject(runtimeclient.Object)
}

// Result contains the result of a Reconciler invocation.
type Result struct {
	// Requeue tells the Controller to requeue the reconcile key.  Defaults to false.
	Requeue bool

	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	RequeueAfter time.Duration
}

// Reconciler primarily define how to reconcile resource events. When you are implementing
// business logic, this is what you will spend most of your time working on.
type Reconciler interface {
	// Reconcile performs the reconciliation. It takes the meta namespace key of the object
	// to reconcile as the argument. It returns an error if and only if reconciliation should
	// retry due to an error.
	Reconcile(ctx context.Context, key string) (Result, error)
}

// GetKeyFunc is a function that parses resource key from a resource event object.
type GetKeyFunc func(obj interface{}) (string, error)

// Options configures a controller.
type Options struct {
	// Name is the name of the controller. It appears in logs and metrics, so make sure you give it
	// a recognizable name. If no name was given, it uses the resource kind by default.
	Name string
	// Informers is used to create informers for the resources we want to watch.
	Informers runtimecache.Informers
	// Resource is the sample of the resource of control.
	Resource runtimeclient.Object
	// Reconciler is used by all internal workers to reconcile resource events.
	Reconciler Reconciler
	// Predicate is used to filter resource events before they are passed to the Reconciler.
	Predicate Predicate
	// ResyncPeriod is the optional resync period set specifically for the main resource.
	ResyncPeriod *time.Duration
	// MaxWorkerOPS is the maximum number of reconciliation operations each worker can perform per
	// second.
	MaxWorkerOPS float64
	// WatchOptions describes additional resources to watch.
	WatchOptions []WatchOption
}

// GetName returns the proper name for the controller. If Name is not set, it would try to parse the
// resource kind for the watched resource.
func (o *Options) GetName() string {
	if o.Name != "" {
		return o.Name
	}
	gvk, err := apiutil.GVKForObject(o.Resource, client.Scheme)
	if err != nil {
		return "<UNKNOWN>"
	}
	return gvk.GroupKind().String()
}

// WatchOption describes a resource to watch in addition to the controlled resource.
type WatchOption struct {
	Resource     runtimeclient.Object
	Predicate    Predicate
	GetKeyFunc   GetKeyFunc
	ResyncPeriod *time.Duration
}

func newEnqueueHandler(
	queue k8workqueue.Interface,
	getKeyFunc GetKeyFunc,
	predicate Predicate,
	logger klog.Logger,
) kubecache.ResourceEventHandler {
	if predicate == nil {
		predicate = &PredicateFunc{}
	}
	return &enqueueHandler{
		queue:      queue,
		getKeyFunc: getKeyFunc,
		predicate:  predicate,
		logger:     logger,
	}
}

type enqueueHandler struct {
	queue      k8workqueue.Interface
	getKeyFunc GetKeyFunc
	predicate  Predicate
	logger     klog.Logger
}

func (eh *enqueueHandler) OnAdd(obj interface{}) {
	if eh.predicate == nil || eh.predicate.Add(obj) {
		key, err := eh.getKeyFunc(obj)
		if err != nil {
			eh.eventLogger("Add").Error(err, "failed to parse resource key")
			return
		}
		eh.queue.Add(key)
		eh.eventLogger("Add").V(4).Info("resource event handled", "key", key)
	}
}

func (eh *enqueueHandler) OnUpdate(oldObj, newObj interface{}) {
	if eh.predicate == nil || eh.predicate.Update(oldObj, newObj) {
		key, err := eh.getKeyFunc(newObj)
		if err != nil {
			eh.eventLogger("Update").Error(err, "failed to parse resource key")
			return
		}
		eh.queue.Add(key)
		eh.eventLogger("Update").V(4).Info("resource event handled", "key", key)
	}
}

func (eh *enqueueHandler) OnDelete(obj interface{}) {
	if eh.predicate == nil || eh.predicate.Delete(obj) {
		key, err := eh.getKeyFunc(obj)
		if err != nil {
			eh.eventLogger("Delete").Error(err, "failed to parse resource key")
			return
		}
		eh.queue.Add(key)
		eh.eventLogger("Delete").V(4).Info("resource event handled", "key", key)
	}
}

func (eh *enqueueHandler) eventLogger(event string) klog.Logger {
	return eh.logger.WithValues("event", event)
}

func validateOptions(options *Options) error {
	if options.Reconciler == nil {
		return errors.New("nil reconciler")
	}
	if options.Resource == nil {
		return errors.New("nil resource")
	}
	return nil
}
