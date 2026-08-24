// Package arbiter implements the oxidation/deterioration recheck and final
// arbiter: recheck generations, isolation suggestions, independent review
// records and the single final credential.
package arbiter

import (
	"github.com/olivepress/fruit-intake-gate/task"
)

// ReviewRole is the role a reviewer plays in independent review.
type ReviewRole string

// The two independent review roles required before finalization.
const (
	RolePrimary   ReviewRole = "primary"
	RoleSecondary ReviewRole = "secondary"
)

// ReviewDecision is a single reviewer's decision on the recheck evidence.
type ReviewDecision string

const (
	DecisionApprove ReviewDecision = "approve"
	DecisionReject  ReviewDecision = "reject"
)

// Review records one independent review for a task generation.
type Review struct {
	TaskID         task.TaskID
	Generation     task.Generation
	ReviewerID     string
	Role           ReviewRole
	Decision       ReviewDecision
	EvidenceDigest string
}

// Credential is the unique final credential written by the single-write
// barrier once a task is finalized.
type Credential struct {
	TaskID     task.TaskID
	Generation task.Generation
	Kind       task.FinalKind
	Digest     string
}

// IsIndependentPair reports whether two reviews form a valid independent
// pair: two distinct qualified reviewers with no role overlap.
func IsIndependentPair(a, b Review) bool {
	if a.ReviewerID == b.ReviewerID {
		return false
	}
	if a.Role == b.Role {
		return false
	}
	if a.TaskID != b.TaskID || a.Generation != b.Generation {
		return false
	}
	return true
}
