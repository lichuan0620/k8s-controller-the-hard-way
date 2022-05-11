// nolint:revive
package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo"
	demov1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	clientset "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/clientset/versioned"
	demov1informers "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/informers/externalversions/demo/v1"
	demov1listers "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/listers/demo/v1"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/reconciliation"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"
)

const (
	redisPortName = "redis"
	redisPort     = 6379
)

const (
	apiVersion = demo.GroupName + "/" + demov1.Version
	kind       = "Redis"
)

type RedisController struct {
	k8client   kubernetes.Interface
	demoClient clientset.Interface

	stsLister   appsv1listers.StatefulSetLister
	svcLister   corev1listers.ServiceLister
	redisLister demov1listers.RedisLister

	notifyTenantUpdate func(string)

	queue workqueue.RateLimitingInterface

	concurrency      int
	reconcileTimeout time.Duration
	logger           klog.Logger
}

func NewRedisController(
	k8client kubernetes.Interface,
	demoClient clientset.Interface,
	stsInformer appsv1informers.StatefulSetInformer,
	svcInformer corev1informers.ServiceInformer,
	redisInformer demov1informers.RedisInformer,
	notifyTenantUpdate func(string),
	concurrency int,
	reconcileTimeout time.Duration,
) *RedisController {
	const moduleName = "redis-controller"
	queue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), moduleName)
	logger := klog.NewKlogr().WithName(moduleName)
	redisInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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
				queue.Add(newMeta.GetNamespace() + "/" + newMeta.GetName())
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
	isOwnedByRedisObject := func(o metav1.Object) bool {
		ownerReferences := o.GetOwnerReferences()
		for i := range ownerReferences {
			if or := ownerReferences[i]; or.APIVersion == apiVersion && or.Kind == kind {
				return true
			}
		}
		return false
	}
	childrenHandler := cache.ResourceEventHandlerFuncs{
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
			if oldMeta.GetResourceVersion() != newMeta.GetResourceVersion() &&
				(isOwnedByRedisObject(oldMeta) || isOwnedByRedisObject(newMeta)) {
				queue.Add(newMeta.GetNamespace() + "/" + newMeta.GetName())
			}
		},
	}
	stsInformer.Informer().AddEventHandler(childrenHandler)
	svcInformer.Informer().AddEventHandler(childrenHandler)
	return &RedisController{
		k8client:           k8client,
		demoClient:         demoClient,
		stsLister:          stsInformer.Lister(),
		svcLister:          svcInformer.Lister(),
		redisLister:        redisInformer.Lister(),
		notifyTenantUpdate: notifyTenantUpdate,
		queue:              queue,
		concurrency:        concurrency,
		reconcileTimeout:   reconcileTimeout,
		logger:             logger,
	}
}

func (c *RedisController) Run(ctx context.Context) {
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

func (c *RedisController) processItemFromQueue() {
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

func (c *RedisController) reconcile(ctx context.Context, key string) (err error) {
	var namespace, name string
	namespace, name, err = cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Error(err, "failed to split meta namespace key; discarding invalid event")
		return nil
	}
	var redis *demov1.Redis
	redis, err = c.redisLister.Redises(namespace).Get(name)
	if err != nil {
		// if the object cannot be found, it probably has been deleted
		if k8errors.IsNotFound(err) {
			return nil
		}
		return errors.Wrap(err, "get Redis")
	}
	if redis.Spec.Pause {
		return nil
	}
	if redis.DeletionTimestamp != nil && !redis.DeletionTimestamp.IsZero() {
		if tenant := redis.Labels[demov1.LabelTenant]; tenant != "" {
			c.notifyTenantUpdate(tenant)
		}
		return nil
	}
	var ready bool
	defer func() {
		statusUpdated := false
		if redis.Status == nil {
			statusUpdated = true
			redis.Status = &demov1.RedisStatus{}
		}
		if ready != redis.Status.Ready {
			statusUpdated = true
			redis.Status.Ready = ready
		}
		if statusUpdated {
			_, statusErr := c.demoClient.DemoV1().Redises(namespace).UpdateStatus(ctx, redis, metav1.UpdateOptions{})
			if statusErr != nil && err == nil {
				err = errors.Wrap(statusErr, "update Redis status")
			}
			if tenant := redis.Labels[demov1.LabelTenant]; tenant != "" {
				c.notifyTenantUpdate(tenant)
			}
		}
	}()
	wg, _ := errgroup.WithContext(ctx)
	wg.Go(func() error {
		var localErr error
		ready, localErr = c.createOrUpdateStatefulSet(ctx, redis)
		return errors.Wrap(localErr, "create-update StatefulSet")
	})
	wg.Go(func() error {
		return errors.Wrap(c.createOrUpdateService(ctx, redis), "create-update Service")
	})
	if err = wg.Wait(); err != nil {
		return err
	}
	return nil
}

func (c *RedisController) createOrUpdateStatefulSet(ctx context.Context, src *demov1.Redis) (ready bool, err error) {
	created := buildRedisStatefulSet(src)
	existed, err := c.stsLister.StatefulSets(src.Namespace).Get(src.Name)
	if err != nil {
		if k8errors.IsNotFound(err) {
			_, err = c.k8client.AppsV1().StatefulSets(src.Namespace).Create(ctx, created, metav1.CreateOptions{})
		}
		return false, err
	}
	if len(existed.OwnerReferences) > 0 && reconciliation.IndexOwnerRef(existed.OwnerReferences, created.OwnerReferences[0]) == -1 {
		return false, errors.New("resource already has a different owner")
	}
	mergeRedisStatefulSet(created, existed)
	if existed, err = c.k8client.AppsV1().StatefulSets(src.Namespace).Update(ctx, existed, metav1.UpdateOptions{}); err != nil {
		return false, err
	}
	return existed.Status.ReadyReplicas > 0, nil
}

func (c *RedisController) createOrUpdateService(ctx context.Context, src *demov1.Redis) error {
	created := buildRedisService(src)
	existed, err := c.svcLister.Services(src.Namespace).Get(src.Name)
	if err != nil {
		if k8errors.IsNotFound(err) {
			_, err = c.k8client.CoreV1().Services(src.Namespace).Create(ctx, created, metav1.CreateOptions{})
		}
		return err
	}
	if len(existed.OwnerReferences) > 0 && reconciliation.IndexOwnerRef(existed.OwnerReferences, created.OwnerReferences[0]) == -1 {
		b, _ := yaml.Marshal(existed.OwnerReferences)
		fmt.Println("existed service owner reference:\n" + string(b))
		b, _ = yaml.Marshal(created.OwnerReferences)
		fmt.Println("created service owner reference:\n" + string(b))
		return errors.New("resource already has a different owner")
	}
	mergeRedisService(created, existed)
	_, err = c.k8client.CoreV1().Services(src.Namespace).Update(ctx, existed, metav1.UpdateOptions{})
	return err
}

func buildRedisStatefulSet(src *demov1.Redis) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      src.Name,
			Namespace: src.Namespace,
			Labels: map[string]string{
				demov1.LabelRedis: src.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         apiVersion,
				Kind:               kind,
				Name:               src.Name,
				UID:                src.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			}},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: pointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					demov1.LabelRedis: src.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						demov1.LabelRedis: src.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "redis",
						Image:           src.Spec.Image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports: []corev1.ContainerPort{
							{
								Name:          redisPortName,
								ContainerPort: redisPort,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						Resources: corev1.ResourceRequirements{
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    *resource.NewQuantity(1, resource.DecimalSI),
								corev1.ResourceMemory: *resource.NewQuantity(2147483648, resource.BinarySI),
							},
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceCPU:    *resource.NewQuantity(1, resource.DecimalSI),
								corev1.ResourceMemory: *resource.NewQuantity(1073741824, resource.BinarySI),
							},
						},
					}},
				},
			},
			ServiceName: src.Name,
		},
	}
}

func mergeRedisStatefulSet(created, toUpdate *appsv1.StatefulSet) {
	if toUpdate.Labels == nil {
		toUpdate.Labels = map[string]string{}
	}
	toUpdate.Labels[demov1.LabelRedis] = created.Name
	toUpdate.OwnerReferences = reconciliation.UpsertOwnerRef(created.OwnerReferences[0], toUpdate.OwnerReferences)
	toUpdate.Spec.Replicas = pointer.Int32(1)
	toUpdate.Spec.Selector = created.Spec.Selector
	toUpdate.Spec.Template.ObjectMeta = created.Spec.Template.ObjectMeta
	toUpdate.Spec.Template.Spec.Containers = created.Spec.Template.Spec.Containers
	toUpdate.Spec.ServiceName = created.Name
}

func buildRedisService(src *demov1.Redis) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      src.Name,
			Namespace: src.Namespace,
			Labels: map[string]string{
				demov1.LabelRedis: src.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         apiVersion,
				Kind:               kind,
				Name:               src.Name,
				UID:                src.UID,
				Controller:         pointer.BoolPtr(true),
				BlockOwnerDeletion: pointer.BoolPtr(true),
			}},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       redisPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       redisPort,
				TargetPort: intstr.FromString(redisPortName),
			}},
			Selector: map[string]string{
				demov1.LabelRedis: src.Name,
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

func mergeRedisService(created, toUpdate *corev1.Service) {
	if toUpdate.Labels == nil {
		toUpdate.Labels = map[string]string{}
	}
	toUpdate.Labels[demov1.LabelRedis] = created.Name
	toUpdate.OwnerReferences = reconciliation.UpsertOwnerRef(created.OwnerReferences[0], toUpdate.OwnerReferences)
	portFound := false
	for i := range toUpdate.Spec.Ports {
		if toUpdate.Spec.Ports[i].Name == redisPortName {
			portFound = true
			toUpdate.Spec.Ports[i] = created.Spec.Ports[0]
			break
		}
	}
	if !portFound {
		toUpdate.Spec.Ports = append(toUpdate.Spec.Ports, created.Spec.Ports[0])
	}
	toUpdate.Spec.Selector = created.Spec.Selector
}
