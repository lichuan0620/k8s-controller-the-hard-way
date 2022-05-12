// nolint:revive
package reconcilers

import (
	"context"
	"time"

	demov1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/controller"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	redisPortName = "redis"
	redisPort     = 6379
)

// ParseRedisKeyFromObj parses Redis resource key from the given object. Said object must not
// be in the deleted state and must have the necessary label.
func ParseRedisKeyFromObj(obj interface{}) (string, error) {
	if _, isDeleted := obj.(cache.DeletedFinalStateUnknown); isDeleted {
		return "", errors.New("object is in the unknown deleted state")
	}
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return "", err
	}
	name := metaObj.GetLabels()[demov1.LabelRedis]
	if name == "" {
		return "", errors.New("missing " + demov1.LabelRedis + " label")
	}
	return metaObj.GetNamespace() + "/" + name, nil
}

func NewRedisReconciler(
	notifyTenantUpdate func(string),
	c runtimeclient.Client,
	reconcileTimeout time.Duration,
) controller.Reconciler {
	gvk, _ := apiutil.GVKForObject(&demov1.Redis{}, client.Scheme)
	gk := gvk.GroupKind().String()
	logger := klog.NewKlogr().WithName(gk).WithValues("resource", gk)
	return &redisReconciler{
		notifyTenantUpdate: notifyTenantUpdate,
		client:             c,
		reconcileTimeout:   reconcileTimeout,
		logger:             logger,
	}
}

type redisReconciler struct {
	notifyTenantUpdate func(string)

	client           runtimeclient.Client
	reconcileTimeout time.Duration
	logger           klog.Logger
}

func (r *redisReconciler) Reconcile(ctx context.Context, key string) (re controller.Result, err error) {
	var objectKey runtimeclient.ObjectKey
	objectKey.Namespace, objectKey.Name, err = cache.SplitMetaNamespaceKey(key)
	if err != nil {
		r.logger.Error(err, "failed to split meta namespace key; discarding invalid event")
		return controller.Result{}, nil
	}
	var redis demov1.Redis
	if err = r.client.Get(ctx, objectKey, &redis); err != nil {
		// if the object cannot be found, it probably has been deleted
		if k8errors.IsNotFound(err) {
			return controller.Result{}, nil
		}
		return controller.Result{}, errors.Wrap(err, "get Redis")
	}
	if redis.Spec.Pause {
		return controller.Result{}, nil
	}
	if redis.DeletionTimestamp != nil && !redis.DeletionTimestamp.IsZero() {
		if tenant := redis.Labels[demov1.LabelTenant]; tenant != "" {
			r.notifyTenantUpdate(tenant)
		}
		return controller.Result{}, nil
	}
	var ready bool
	defer func() {
		updated := false
		if redis.Status == nil {
			redis.Status = &demov1.RedisStatus{}
			updated = true
		}
		if ready != redis.Status.Ready {
			redis.Status.Ready = ready
			updated = true
		}
		if updated {
			if statusErr := r.client.Status().Update(ctx, &redis); statusErr != nil && err == nil {
				err = errors.Wrap(statusErr, "update Redis status")
			}
			if tenant := redis.Labels[demov1.LabelTenant]; tenant != "" {
				r.notifyTenantUpdate(tenant)
			}
		}
	}()
	wg, _ := errgroup.WithContext(ctx)
	wg.Go(func() error {
		var localErr error
		ready, localErr = r.createOrUpdateStatefulSet(ctx, &redis)
		return errors.Wrap(localErr, "create-update StatefulSet")
	})
	wg.Go(func() error {
		return errors.Wrap(r.createOrUpdateService(ctx, &redis), "create-update Service")
	})
	if err = wg.Wait(); err != nil {
		if _, isAlreadyOwnedError := err.(*controllerutil.AlreadyOwnedError); isAlreadyOwnedError {
			r.logger.Error(err, "resource already has an owner, giving up reconciliation", "key", key)
			return controller.Result{}, nil
		}
		return controller.Result{}, err
	}
	return controller.Result{}, nil
}

func (r *redisReconciler) createOrUpdateStatefulSet(ctx context.Context, src *demov1.Redis) (ready bool, err error) {
	sts := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      src.Name,
			Namespace: src.Namespace,
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, r.client, &sts, func() error {
		if sts.Labels == nil {
			sts.Labels = map[string]string{}
		}
		sts.Labels[demov1.LabelRedis] = src.Name
		if err := controllerutil.SetControllerReference(src, &sts, r.client.Scheme()); err != nil {
			return err
		}
		sts.Spec.Replicas = pointer.Int32(1)
		sts.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: map[string]string{
				demov1.LabelRedis: src.Name,
			},
		}
		if sts.Spec.Template.ObjectMeta.Labels == nil {
			sts.Spec.Template.ObjectMeta.Labels = map[string]string{}
		}
		sts.Spec.Template.ObjectMeta.Labels[demov1.LabelRedis] = src.Name
		sts.Spec.Template = corev1.PodTemplateSpec{
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
		}
		sts.Spec.ServiceName = src.Name
		return nil
	})
	if err != nil {
		return false, err
	}
	return sts.Status.ReadyReplicas == sts.Status.Replicas, nil
}

func (r *redisReconciler) createOrUpdateService(ctx context.Context, src *demov1.Redis) error {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      src.Name,
			Namespace: src.Namespace,
		},
	}
	_, err := controllerutil.CreateOrPatch(ctx, r.client, &svc, func() error {
		if svc.Labels == nil {
			svc.Labels = map[string]string{}
		}
		svc.Labels[demov1.LabelRedis] = src.Name
		if err := controllerutil.SetControllerReference(src, &svc, r.client.Scheme()); err != nil {
			return err
		}
		desiredPort := corev1.ServicePort{
			Name:       redisPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       redisPort,
			TargetPort: intstr.FromString(redisPortName),
		}
		portFound := false
		for i := range svc.Spec.Ports {
			if svc.Spec.Ports[i].Name == redisPortName {
				portFound = true
				svc.Spec.Ports[i] = desiredPort
				break
			}
		}
		if !portFound {
			svc.Spec.Ports = append(svc.Spec.Ports, desiredPort)
		}
		svc.Spec.Selector = map[string]string{
			demov1.LabelRedis: src.Name,
		}
		if svc.Spec.Type == "" {
			svc.Spec.Type = corev1.ServiceTypeClusterIP
		}
		return nil
	})
	return err
}
