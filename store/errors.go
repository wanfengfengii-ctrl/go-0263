// Package store is the SQLite persistence runtime: stable error codes, the
// Store interface, transaction boundaries and restart recovery.
package store

import (
	"errors"
	"fmt"
)

// Stable error codes surfaced by the HTTP API.
const (
	CodeStaleRuleDigest           = "ERR_STALE_RULE_DIGEST"
	CodePlotCultivarWindow        = "ERR_PLOT_CULTIVAR_WINDOW"
	CodeDuplicateSeal             = "ERR_DUPLICATE_SEAL"
	CodeDuplicateBlindCode        = "ERR_DUPLICATE_BLIND_CODE"
	CodeIntakeBatchDuplicate      = "ERR_INTAKE_BATCH_DUPLICATE"
	CodeResourceBusy              = "ERR_RESOURCE_BUSY"
	CodeOperationConflict         = "ERR_OPERATION_CONFLICT"
	CodeGenerationMismatch        = "ERR_GENERATION_MISMATCH"
	CodeCountNotConserved         = "ERR_COUNT_NOT_CONSERVED"
	CodeFixedPointInvalid         = "ERR_FIXED_POINT_INVALID"
	CodeFixedPointOverflow        = "ERR_FIXED_POINT_OVERFLOW"
	CodeAdapterRetryPending       = "ERR_ADAPTER_RETRY_PENDING"
	CodeRejudgeGenerationConflict = "ERR_REJUDGE_GENERATION_CONFLICT"
	CodeRoleOverlap               = "ERR_ROLE_OVERLAP"
	CodeTerminalState             = "ERR_TERMINAL_STATE"
	CodeReviewerNotQualified      = "ERR_REVIEWER_NOT_QUALIFIED"
	CodeInvalidState              = "ERR_INVALID_STATE"
	CodeNotFound                  = "ERR_NOT_FOUND"
	CodeInvalidRequest            = "ERR_INVALID_REQUEST"
)

// CodedError pairs a stable code with a human-readable reason and an optional,
// deterministically sorted list of multi-cause reasons.
type CodedError struct {
	Code   string
	Reason string
	Causes []string
}

func (e *CodedError) Error() string {
	if e.Reason == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Reason)
}

// NewCodedError builds a stable coded error with optional sorted causes.
func NewCodedError(code, reason string, causes ...string) *CodedError {
	return &CodedError{Code: code, Reason: reason, Causes: causes}
}

// ErrNotFound is returned when a task does not exist.
var ErrNotFound = errors.New("store: not found")

// Internal sentinel errors mapped to stable codes by mapErr.
var (
	errInvalidState         = errors.New("store: invalid state")
	errDuplicateSeal        = errors.New("store: duplicate crate seal")
	errDuplicateBlindCode   = errors.New("store: duplicate blind code")
	errIntakeBatchDuplicate = errors.New("store: duplicate intake batch")
	errOperationConflict    = errors.New("store: operation conflict")
)
