package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Redis describes a managed Redis instance.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=redises
// +kubebuilder:printcolumn:name="Replica",type=string,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:subresource:status
type Redis struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              RedisSpec    `json:"spec"`
	Status            *RedisStatus `json:"status,omitempty"`
}

// RedisSpec defines the specification of a Redis object.
type RedisSpec struct {
	// +kubebuilder:default:="redis:latest"
	// +kubebuilder:validation:Optional
	Image string `json:"image"`
	Pause bool   `json:"pause,omitempty"`
}

// RedisStatus describes the status of a Redis object.
type RedisStatus struct {
	Ready bool `json:"ready"`
}

// RedisList describes a list of Redis objects.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type RedisList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Redis `json:"items"`
}

// TenantState describes the state of a tenant.
// +kubebuilder:validation:Enum:=Active;Suspended
type TenantState string

const (
	// TenantStateActive means the tenant is allowed to use its resources.
	TenantStateActive = "Active"
	// TenantStateSuspended means the tenant is forbidden from using its resources.
	TenantStateSuspended = "Suspended"
)

// Tenant describe a user that owns other resources.
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	// +kubebuilder:default:=Active
	// +kubebuilder:validation:Optional
	State TenantState `json:"state"`
	// +kubebuilder:validation:Minimum:=0
	OwnedRedisCount int `json:"ownedRedisCount,omitempty"`
}

// TenantList describes a list of Tenant objects.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Tenant `json:"items"`
}
