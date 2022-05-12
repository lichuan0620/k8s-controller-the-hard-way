package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Predicate filters events before enqueuing the keys.
type Predicate interface {
	// Add returns true if the Add event should be processed.
	Add(obj interface{}) bool
	// Update returns true if the Update event should be processed.
	Update(oldObj, newObj interface{}) bool
	// Delete returns true if the Delete event should be processed.
	Delete(obj interface{}) bool
}

// Predicates is a ordered list of Predicates. It can be used as a single Predicate in which
// all members are executed in the listed order.
type Predicates []Predicate

// Add implements the Predicate interface.
func (p Predicates) Add(obj interface{}) bool {
	for i := range p {
		if !p[i].Add(obj) {
			return false
		}
	}
	return true
}

// Update implements the Predicate interface.
func (p Predicates) Update(oldObj, newObj interface{}) bool {
	for i := range p {
		if !p[i].Update(oldObj, newObj) {
			return false
		}
	}
	return true
}

// Delete implements the Predicate interface.
func (p Predicates) Delete(obj interface{}) bool {
	for i := range p {
		if !p[i].Delete(obj) {
			return false
		}
	}
	return true
}

// PredicateFunc implements Predicate interface. Unset functions return true for all events.
type PredicateFunc struct {
	// Add returns true if the Add event should be processed.
	AddFunc func(obj interface{}) bool
	// Update returns true if the Update event should be processed.
	UpdateFunc func(oldObj, newObj interface{}) bool
	// Delete returns true if the Delete event should be processed.
	DeleteFunc func(obj interface{}) bool
}

// Add implements Predicate interface.
func (p PredicateFunc) Add(obj interface{}) bool {
	if p.AddFunc == nil {
		return true
	}
	return p.AddFunc(obj)
}

// Update implements Predicate interface.
func (p PredicateFunc) Update(oldObj, newObj interface{}) bool {
	if p.UpdateFunc == nil {
		return true
	}
	return p.UpdateFunc(oldObj, newObj)
}

// Delete implements Predicate interface.
func (p PredicateFunc) Delete(obj interface{}) bool {
	if p.DeleteFunc == nil {
		return true
	}
	return p.DeleteFunc(obj)
}

// ResourceVersionPredicate return a Predicate object that filters Update events whose new
// and old objects have identical resource version.
func ResourceVersionPredicate() Predicate {
	return PredicateFunc{
		UpdateFunc: func(oldObj, newObj interface{}) bool {
			oldMeta, err := meta.Accessor(oldObj)
			if err != nil {
				return false
			}
			newMeta, err := meta.Accessor(newObj)
			if err != nil {
				return false
			}
			newRV := newMeta.GetResourceVersion()
			return len(newRV) == 0 || oldMeta.GetResourceVersion() != newRV
		},
	}
}

// GenerationPredicate returns a Predicate object that filters Update events whose new and
// old objects have identical generation number.
func GenerationPredicate() Predicate {
	return PredicateFunc{
		UpdateFunc: func(oldObj, newObj interface{}) bool {
			oldMeta, err := meta.Accessor(oldObj)
			if err != nil {
				return false
			}
			newMeta, err := meta.Accessor(newObj)
			if err != nil {
				return false
			}
			return oldMeta.GetGeneration() != newMeta.GetGeneration()
		},
	}
}

// AnnotationPredicate returns a Predicate that only allow resources with annotations
// matched by the given selector.
func AnnotationPredicate(selector labels.Selector) Predicate {
	match := func(obj interface{}) bool {
		o, err := meta.Accessor(obj)
		if err != nil {
			// might be delete event
			return true
		}
		return selector.Matches(labels.Set(o.GetAnnotations()))
	}
	return PredicateFunc{
		AddFunc: match,
		UpdateFunc: func(_, obj interface{}) bool {
			return match(obj)
		},
		DeleteFunc: match,
	}
}

// OwnerPredicate returns a Predicate object that only allow update events with resources
// with owners of the given kind.
func OwnerPredicate(kind schema.GroupVersionKind) Predicate {
	isOwnedByKind := func(o metav1.Object) bool {
		ownerReferences := o.GetOwnerReferences()
		for i := range ownerReferences {
			if or := ownerReferences[i]; or.APIVersion == kind.GroupVersion().String() && or.Kind == kind.Kind {
				return true
			}
		}
		return false
	}
	return PredicateFunc{
		AddFunc: func(_ interface{}) bool { return false },
		UpdateFunc: func(oldObj, newObj interface{}) bool {
			oldMeta, err := meta.Accessor(oldObj)
			if err != nil {
				return false
			}
			newMeta, err := meta.Accessor(newObj)
			if err != nil {
				return false
			}
			return isOwnedByKind(oldMeta) || isOwnedByKind(newMeta)
		},
		DeleteFunc: func(_ interface{}) bool { return false },
	}
}
