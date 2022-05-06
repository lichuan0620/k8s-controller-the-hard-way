package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Redis describes a managed Redis instance.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:printcolumn:name="Replica",type=string,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:subresource:status
type Redis struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              RedisSpec    `json:"spec"`
	Status            *RedisStatus `json:"status"`
}

type RedisSpec struct {
	// +kubebuilder:default:="redis:latest"
	Image      string             `json:"image"`
	Containers []corev1.Container `json:"containers,omitempty"`
	Pause      bool               `json:"pause,omitempty"`
}

type RedisStatus struct {
	Paused          bool  `json:"paused,omitempty"`
	Replicas        int32 `json:"replicas"`
	ReadyReplicas   int32 `json:"readyReplicas,omitempty"`
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`
}
