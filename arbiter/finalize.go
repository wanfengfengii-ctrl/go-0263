package arbiter

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/olivepress/fruit-intake-gate/task"
)

// Sentinel errors for the final single-write barrier.
var (
	ErrInsufficientReview = errors.New("arbiter: insufficient independent approval")
	ErrRecheckUnresolved  = errors.New("arbiter: unresolved recheck blocks cold-press")
	ErrNoIsolationBasis   = errors.New("arbiter: no isolation basis for isolation outcome")
	ErrFinalConflict      = errors.New("arbiter: final outcome already committed")
)

// ValidateFinalize checks whether the requested terminal kind is consistent
// with the accumulated reviews and recheck evidence.
//
//   - cancellation is always permitted from a non-terminal state;
//   - cold-press requires two independent approvals and no unresolved recheck;
//   - isolation requires a deterioration recheck as its basis.
func ValidateFinalize(kind task.FinalKind, reviews ReviewSet, hasRecheck bool) error {
	switch kind {
	case task.FinalCancellation:
		return nil
	case task.FinalColdPress:
		if hasRecheck {
			return ErrRecheckUnresolved
		}
		if !reviews.HasIndependentApproval() {
			return ErrInsufficientReview
		}
		return nil
	case task.FinalIsolation:
		if !hasRecheck {
			return ErrNoIsolationBasis
		}
		return nil
	default:
		return fmt.Errorf("arbiter: unknown final kind %q", kind)
	}
}

// FinalBarrier enforces the single-write barrier over a task generation.
type FinalBarrier struct {
	committed map[task.TaskID]Credential
}

// NewFinalBarrier returns an empty final barrier.
func NewFinalBarrier() *FinalBarrier {
	return &FinalBarrier{committed: make(map[task.TaskID]Credential)}
}

// Commit writes the unique final credential for a task. Exactly one outcome
// can commit per task; a second commit returns ErrFinalConflict.
func (b *FinalBarrier) Commit(c Credential) error {
	if _, ok := b.committed[c.TaskID]; ok {
		return ErrFinalConflict
	}
	b.committed[c.TaskID] = c
	return nil
}

// Credential returns the committed credential for a task.
func (b *FinalBarrier) Credential(id task.TaskID) (Credential, bool) {
	c, ok := b.committed[id]
	return c, ok
}

// BuildCredential derives the final credential digest from the task, terminal
// kind and evidence digest.
func BuildCredential(id task.TaskID, gen task.Generation, kind task.FinalKind, evidenceDigest string) Credential {
	sum := sha256.Sum256([]byte(string(id) + ":" + fmt.Sprint(gen) + ":" + string(kind) + ":" + evidenceDigest))
	return Credential{
		TaskID:     id,
		Generation: gen,
		Kind:       kind,
		Digest:     hex.EncodeToString(sum[:]),
	}
}
