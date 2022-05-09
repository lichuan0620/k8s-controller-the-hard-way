// nolint:revive
package v1

import "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo"

const (
	FinalizerOwnedResource = demo.GroupName + "/owned-resource"
)

const (
	LabelTenant = demo.GroupName + "/tenant"
	LabelRedis  = demo.GroupName + "/redis"
)
