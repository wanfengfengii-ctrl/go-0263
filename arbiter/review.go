package arbiter

import (
	"errors"

	"github.com/olivepress/fruit-intake-gate/task"
)

// Sentinel errors for the independent review stage.
var (
	ErrRoleOverlap        = errors.New("arbiter: reviewer role overlap")
	ErrReviewerReused     = errors.New("arbiter: reviewer already recorded for generation")
	ErrNotQualified       = errors.New("arbiter: reviewer not qualified")
	ErrGenerationMismatch = errors.New("arbiter: generation mismatch")
)

// ReviewSet holds the independent reviews recorded for one task generation.
type ReviewSet struct {
	TaskID     task.TaskID
	Generation task.Generation
	Reviews    []Review
}

// Add records a review, rejecting role overlap or a reviewer who has already
// reviewed the same generation.
func (s *ReviewSet) Add(r Review) error {
	if r.TaskID != s.TaskID || r.Generation != s.Generation {
		return ErrGenerationMismatch
	}
	for _, existing := range s.Reviews {
		if existing.ReviewerID == r.ReviewerID {
			return ErrReviewerReused
		}
		if existing.Role == r.Role {
			return ErrRoleOverlap
		}
	}
	s.Reviews = append(s.Reviews, r)
	return nil
}

// HasIndependentApproval reports whether two distinct reviewers with distinct
// roles have both approved the current generation.
func (s *ReviewSet) HasIndependentApproval() bool {
	if len(s.Reviews) < 2 {
		return false
	}
	for i := 0; i < len(s.Reviews); i++ {
		for j := i + 1; j < len(s.Reviews); j++ {
			if IsIndependentPair(s.Reviews[i], s.Reviews[j]) {
				return true
			}
		}
	}
	return false
}

// HasReject reports whether any reviewer rejected the evidence.
func (s *ReviewSet) HasReject() bool {
	for _, r := range s.Reviews {
		if r.Decision == DecisionReject {
			return true
		}
	}
	return false
}
