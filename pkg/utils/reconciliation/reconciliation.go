package reconciliation

import (
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetStringMustEmpty set the value of `dest` string to equal `src`. `dest` must be an empty
// string or already has the same value as `src`.
func SetStringMustEmpty(src string, dest *string) error {
	if dest == nil {
		return errors.New("setting value to nil string")
	}
	if *dest == "" {
		*dest = src
		return nil
	}
	if *dest != src {
		return errors.New("destination string is not empty")
	}
	return nil
}

// UpsertOwnerRef adds a copy of `src` with `ref` updated or inserted to it.
func UpsertOwnerRef(ref metav1.OwnerReference, src []metav1.OwnerReference) []metav1.OwnerReference {
	idx := IndexOwnerRef(src, ref)
	if idx == -1 {
		src = append(src, ref)
		return src
	}
	ret := make([]metav1.OwnerReference, len(src))
	copy(ret, src)
	ret[idx] = ref
	return ret
}

// IndexOwnerRef returns the index of the owner reference in the slice if found, or -1.
func IndexOwnerRef(ownerReferences []metav1.OwnerReference, ref metav1.OwnerReference) int {
	for index, r := range ownerReferences {
		if ReferSameObject(r, ref) {
			return index
		}
	}
	return -1
}

// ReferSameObject returns true if a and b point to the same object.
func ReferSameObject(a, b metav1.OwnerReference) bool {
	return a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Name == b.Name
}
