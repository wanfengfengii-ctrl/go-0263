package ledger

import (
	"errors"
	"sort"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

// ErrResourceBusy is returned when a resource is already held by another
// active lease.
var ErrResourceBusy = errors.New("ledger: resource busy")

// LeaseSet enforces the at-most-one-active-lease invariant across a set of
// resources, ordered for deterministic conflict reporting.
type LeaseSet struct {
	leases map[string]*Lease
}

// NewLeaseSet returns an empty lease set.
func NewLeaseSet() *LeaseSet {
	return &LeaseSet{leases: make(map[string]*Lease)}
}

func leaseKey(kind catalog.ResourceKind, id string) string {
	return string(kind) + ":" + id
}

// Claim attempts to hold a resource at the given tick. It fails with
// ErrResourceBusy when another active lease already holds the resource.
func (s *LeaseSet) Claim(l *Lease, at Tick) error {
	key := leaseKey(l.ResourceKind, l.ResourceID)
	if existing, ok := s.leases[key]; ok && existing.IsActive(at) {
		return ErrResourceBusy
	}
	s.leases[key] = l
	return nil
}

// Release marks a lease released, freeing the resource for other tasks.
func (s *LeaseSet) Release(kind catalog.ResourceKind, id string, at Tick) {
	key := leaseKey(kind, id)
	if l, ok := s.leases[key]; ok && l.ReleasedTick == nil {
		t := at
		l.ReleasedTick = &t
	}
}

// ActiveConflicts returns the sorted resource keys whose leases are still
// active at the given tick, for deterministic reason serialization.
func (s *LeaseSet) ActiveConflicts(at Tick) []string {
	var out []string
	for key, l := range s.leases {
		if l.IsActive(at) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// OccupiedBy reports whether any active lease for the resource is held by a
// task other than the given one.
func (s *LeaseSet) OccupiedBy(kind catalog.ResourceKind, id string, t task.TaskID, at Tick) bool {
	if l, ok := s.leases[leaseKey(kind, id)]; ok {
		return l.IsActive(at) && l.TaskID != t
	}
	return false
}
