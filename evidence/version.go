package evidence

import (
	"github.com/olivepress/fruit-intake-gate/task"
)

// EvidenceKind classifies an immutable evidence version.
type EvidenceKind string

// The evidence kinds recorded in the append-only version chain.
const (
	EvidenceReading       EvidenceKind = "reading"
	EvidenceMaturity      EvidenceKind = "maturity"
	EvidenceForeignMatter EvidenceKind = "foreign-matter"
	EvidenceRecheck       EvidenceKind = "recheck"
	EvidenceFinal         EvidenceKind = "final"
)

// EvidenceVersion is one immutable row in the append-only evidence chain. A
// version is never overwritten: late evidence for an older generation is
// appended with Accepted=false and never rewrites current-generation state.
type EvidenceVersion struct {
	TaskID       task.TaskID
	Generation   task.Generation
	EvidenceKind EvidenceKind
	SubjectKey   string
	Version      int
	FixedValue   int64
	UnitScale    int
	RawDigest    string
	Accepted     bool
	ReasonCode   string
	CreatedTick  int64
}

// VersionChain is an ordered, append-only sequence of evidence versions for a
// single task generation, keyed by evidence kind and subject.
type VersionChain struct {
	versions []EvidenceVersion
}

// Append adds a new version and returns its assigned version number. The
// version number is one greater than the highest existing version for the
// same kind and subject.
func (c *VersionChain) Append(e EvidenceVersion) int {
	next := 1
	for _, v := range c.versions {
		if v.EvidenceKind == e.EvidenceKind && v.SubjectKey == e.SubjectKey && v.Version >= next {
			next = v.Version + 1
		}
	}
	e.Version = next
	c.versions = append(c.versions, e)
	return next
}

// Head returns the highest version for a kind and subject.
func (c *VersionChain) Head(kind EvidenceKind, subject string) (EvidenceVersion, bool) {
	var head EvidenceVersion
	found := false
	for _, v := range c.versions {
		if v.EvidenceKind == kind && v.SubjectKey == subject {
			if !found || v.Version > head.Version {
				head = v
				found = true
			}
		}
	}
	return head, found
}

// IsLate reports whether evidence targets an older generation than the
// current one, which must be recorded as not accepted.
func IsLate(evidenceGen, currentGen task.Generation) bool {
	return evidenceGen < currentGen
}
