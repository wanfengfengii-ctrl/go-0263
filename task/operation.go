package task

import (
	"crypto/sha256"
	"encoding/hex"
)

// OperationKind identifies a mutating business operation on a task. It is the
// first component of an idempotency scope so the same operation number can be
// reused across different operations without collision.
type OperationKind string

// The mutating task operations exposed by the quality gate.
const (
	OpLock           OperationKind = "lock"
	OpSampleConfirm  OperationKind = "sample-confirm"
	OpSplitSamples   OperationKind = "split-samples"
	OpReveal         OperationKind = "reveal"
	OpStartResources OperationKind = "start-resources"
	OpMaturityCounts OperationKind = "maturity-counts"
	OpReadings       OperationKind = "readings"
	OpForeignMatter  OperationKind = "foreign-matter"
	OpRejudge        OperationKind = "rejudge"
	OpReview         OperationKind = "review"
	OpFinalize       OperationKind = "finalize"
)

// OperationNo is a client-supplied idempotency key for a single operation.
type OperationNo string

// RequestDigest is a content hash of a request body used to detect conflicting
// replays of the same operation number.
type RequestDigest string

// DigestPayload hashes an arbitrary payload to a deterministic hex digest.
// Identical payloads produce identical digests; any content difference yields
// a different digest.
func DigestPayload(payload string) RequestDigest {
	sum := sha256.Sum256([]byte(payload))
	return RequestDigest(hex.EncodeToString(sum[:]))
}

// IdempotencyScope builds the stable scope string for an operation number.
func IdempotencyScope(kind OperationKind, id TaskID) string {
	return string(kind) + ":" + string(id)
}
