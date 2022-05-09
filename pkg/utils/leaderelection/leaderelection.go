package leaderelection

import (
	"os"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/uuid"
	coordinationv1client "k8s.io/client-go/kubernetes/typed/coordination/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// NewResourceLock creates a new resource lock for use in a leader election.
func NewResourceLock(
	kubeConfig *rest.Config,
	lockType string,
	lockNamespace string,
	lockLeaseName string,
) (resourcelock.Interface, error) {
	rest.AddUserAgent(kubeConfig, "leader-election")
	corev1Client, err := corev1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, errors.Wrap(err, "build Kubernetes core/v1 client")
	}
	coordinationv1Client, err := coordinationv1client.NewForConfig(kubeConfig)
	if err != nil {
		return nil, errors.Wrap(err, "build Kubernetes coordination/v1 client")
	}
	id, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	id = id + "_" + string(uuid.NewUUID())
	return resourcelock.New(
		lockType,
		lockNamespace,
		lockLeaseName,
		corev1Client,
		coordinationv1Client,
		resourcelock.ResourceLockConfig{
			Identity: id,
		},
	)
}
