// Package catalog provides the olive grove and cold-press rule catalogue:
// fictional plots, cultivars, harvest windows, maturity color grades,
// resource definitions, reviewer qualification and rule digests.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ColorGrade identifies a maturity color grade on the sensory color board.
type ColorGrade string

// The five maturity color grades used for olive fresh-fruit counting.
const (
	ColorGreen       ColorGrade = "green"
	ColorTurning     ColorGrade = "turning"
	ColorPurpleBlack ColorGrade = "purple-black"
	ColorDamaged     ColorGrade = "damaged"
	ColorMoldy       ColorGrade = "moldy"
)

// ValidColorGrades is the canonical ordered set of maturity color grades.
var ValidColorGrades = []ColorGrade{
	ColorGreen,
	ColorTurning,
	ColorPurpleBlack,
	ColorDamaged,
	ColorMoldy,
}

// ResourceKind identifies a physical resource that can be leased by a task.
type ResourceKind string

// The three leasable resource kinds on the cold-press line.
const (
	ResourceCrusherLine ResourceKind = "crusher-line"
	ResourceInertWindow ResourceKind = "inert-window"
	ResourceTestHole    ResourceKind = "test-hole"
)

// PlotID identifies a fictional grove plot.
type PlotID string

// CultivarID identifies an olive cultivar.
type CultivarID string

// ReviewerID identifies a qualified reviewer.
type ReviewerID string

// HarvestPeriod is the inclusive harvest window for a cultivar on a plot.
type HarvestPeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Contains reports whether t falls inside the harvest window (inclusive).
func (p HarvestPeriod) Contains(t time.Time) bool {
	return !t.Before(p.Start) && !t.After(p.End)
}

// Resource is a named resource definition referenced by a rule.
type Resource struct {
	Kind ResourceKind `json:"kind"`
	ID   string       `json:"id"`
}

// RuleDigest is a content-addressed fingerprint of a frozen rule set.
type RuleDigest string

// Plot is a fictional grove plot bound to a cultivar, harvest window and rule.
type Plot struct {
	ID            PlotID        `json:"plot_id"`
	CultivarID    CultivarID    `json:"cultivar_id"`
	HarvestPeriod HarvestPeriod `json:"harvest_period"`
	RuleDigest    RuleDigest    `json:"rule_digest"`
}

// Rule is a frozen cold-press rule set: color grades, resources, reviewers,
// thresholds and foreign-matter screening points, all addressed by a
// deterministic digest.
type Rule struct {
	ID              string       `json:"rule_id"`
	Digest          RuleDigest   `json:"digest"`
	ColorGrades     []ColorGrade `json:"color_grades"`
	Resources       []Resource   `json:"resources"`
	Thresholds      Thresholds   `json:"thresholds"`
	ScreeningPoints []string     `json:"screening_points"`
	ReviewerIDs     []ReviewerID `json:"reviewer_ids"`
}

// ComputeDigest derives the deterministic digest for the receiver's stable
// fields, including thresholds and screening points.
func (r Rule) ComputeDigest() RuleDigest {
	return computeRuleDigest(r.ColorGrades, r.Resources, r.ReviewerIDs, r.Thresholds, r.ScreeningPoints)
}

// ComputeRuleDigest derives a deterministic digest from the canonical
// serialization of a rule's stable fields, ignoring thresholds and screening
// points (retained for the colour-board-only foundation callers).
func ComputeRuleDigest(colorGrades []ColorGrade, resources []Resource, reviewerIDs []ReviewerID) RuleDigest {
	return computeRuleDigest(colorGrades, resources, reviewerIDs, Thresholds{}, nil)
}

func computeRuleDigest(colorGrades []ColorGrade, resources []Resource, reviewerIDs []ReviewerID, thresholds Thresholds, screeningPoints []string) RuleDigest {
	var b strings.Builder
	for _, g := range colorGrades {
		b.WriteString("g:" + string(g) + ";")
	}
	res := append([]Resource(nil), resources...)
	sort.Slice(res, func(i, j int) bool {
		if res[i].Kind != res[j].Kind {
			return res[i].Kind < res[j].Kind
		}
		return res[i].ID < res[j].ID
	})
	for _, r := range res {
		b.WriteString("r:" + string(r.Kind) + ":" + r.ID + ";")
	}
	ids := append([]ReviewerID(nil), reviewerIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		b.WriteString("v:" + string(id) + ";")
	}
	b.WriteString(thresholds.serialize())
	pts := append([]string(nil), screeningPoints...)
	sort.Strings(pts)
	for _, p := range pts {
		b.WriteString("s:" + p + ";")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return RuleDigest(hex.EncodeToString(sum[:]))
}

// Sentinel errors for catalogue-level validation failures.
var (
	ErrStaleRuleDigest      = errors.New("catalog: stale rule digest")
	ErrCultivarMismatch     = errors.New("catalog: cultivar mismatch")
	ErrHarvestWindowMiss    = errors.New("catalog: harvest window mismatch")
	ErrReviewerNotQualified = errors.New("catalog: reviewer not qualified")
)

// ValidateLockInput checks a proposed task lock against the catalogue:
// the rule digest must match, the cultivar must match the plot, the harvest
// shift must fall inside the harvest window, and every reviewer must be
// qualified by the frozen rule set.
func ValidateLockInput(plot Plot, rule Rule, cultivar CultivarID, harvestAt time.Time, reviewerIDs []ReviewerID) error {
	if plot.RuleDigest != rule.Digest {
		return ErrStaleRuleDigest
	}
	if plot.CultivarID != cultivar {
		return ErrCultivarMismatch
	}
	if !plot.HarvestPeriod.Contains(harvestAt) {
		return ErrHarvestWindowMiss
	}
	qualified := make(map[ReviewerID]bool, len(rule.ReviewerIDs))
	for _, id := range rule.ReviewerIDs {
		qualified[id] = true
	}
	for _, id := range reviewerIDs {
		if !qualified[id] {
			return fmt.Errorf("%w: %s", ErrReviewerNotQualified, id)
		}
	}
	return nil
}
