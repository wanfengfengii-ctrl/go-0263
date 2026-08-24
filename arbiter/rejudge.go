package arbiter

import (
	"errors"

	"github.com/olivepress/fruit-intake-gate/task"
)

// RejudgeReason is the trigger for a deterioration recheck.
type RejudgeReason string

// The four recheck triggers recognized by the arbiter.
const (
	RejudgeMaturity      RejudgeReason = "abnormal-maturity"
	RejudgeOxidation     RejudgeReason = "oxidation-breach"
	RejudgeForeignMatter RejudgeReason = "foreign-matter-doubt"
	RejudgeDisagreement  RejudgeReason = "sample-disagreement"
)

// ErrRejudgeConflict is returned when a second, different recheck targets the
// current generation (only one recheck evidence per generation is allowed).
var ErrRejudgeConflict = errors.New("arbiter: rejudge generation conflict")

// Recheck is the single deterioration recheck evidence for a generation. It
// covers the affected crate seals, blind codes and test holes.
type Recheck struct {
	TaskID     task.TaskID
	Generation task.Generation
	Reason     RejudgeReason
	CrateSeals []string
	BlindCodes []string
	TestHoles  []string
}

// SameAs reports whether two rechecks describe the same affected set.
func (r Recheck) SameAs(other Recheck) bool {
	if r.TaskID != other.TaskID || r.Generation != other.Generation || r.Reason != other.Reason {
		return false
	}
	return equalStrings(r.CrateSeals, other.CrateSeals) &&
		equalStrings(r.BlindCodes, other.BlindCodes) &&
		equalStrings(r.TestHoles, other.TestHoles)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}

// RejudgeRegistry holds the at-most-one recheck per generation.
type RejudgeRegistry struct {
	rechecks map[task.Generation]Recheck
}

// NewRejudgeRegistry returns an empty registry.
func NewRejudgeRegistry() *RejudgeRegistry {
	return &RejudgeRegistry{rechecks: make(map[task.Generation]Recheck)}
}

// Record stores the recheck for its generation. Re-recording an identical
// recheck is idempotent; recording a different one for the same generation
// returns ErrRejudgeConflict.
func (r *RejudgeRegistry) Record(recheck Recheck) error {
	if existing, ok := r.rechecks[recheck.Generation]; ok {
		if existing.SameAs(recheck) {
			return nil
		}
		return ErrRejudgeConflict
	}
	r.rechecks[recheck.Generation] = recheck
	return nil
}

// Get returns the recheck for a generation, if recorded.
func (r *RejudgeRegistry) Get(g task.Generation) (Recheck, bool) {
	rc, ok := r.rechecks[g]
	return rc, ok
}
