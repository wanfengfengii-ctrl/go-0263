package task

import (
	"time"

	"github.com/olivepress/fruit-intake-gate/catalog"
)

// Snapshot is the immutable locked snapshot frozen at task creation. It binds
// the plot, cultivar, harvest shift, crate seals, intake batch, crusher line,
// inert window, blind codes, color board, test holes, screening points,
// thresholds and reviewers to the task generation.
type Snapshot struct {
	PlotID          catalog.PlotID       `json:"plot_id"`
	CultivarID      catalog.CultivarID   `json:"cultivar_id"`
	HarvestAt       time.Time            `json:"harvest_at"`
	IntakeBatch     string               `json:"intake_batch"`
	CrateSeals      []string             `json:"crate_seals"`
	BlindCodes      []string             `json:"blind_codes"`
	ColorGrades     []catalog.ColorGrade `json:"color_grades"`
	Resources       []catalog.Resource   `json:"resources"`
	ScreeningPoints []string             `json:"screening_points"`
	Thresholds      catalog.Thresholds   `json:"thresholds"`
	ReviewerIDs     []catalog.ReviewerID `json:"reviewer_ids"`
	RuleDigest      catalog.RuleDigest   `json:"rule_digest"`
}

// HasCrateSeal reports whether the snapshot froze the given crate seal.
func (s Snapshot) HasCrateSeal(seal string) bool {
	for _, c := range s.CrateSeals {
		if c == seal {
			return true
		}
	}
	return false
}

// HasBlindCode reports whether the snapshot froze the given blind code.
func (s Snapshot) HasBlindCode(code string) bool {
	for _, c := range s.BlindCodes {
		if c == code {
			return true
		}
	}
	return false
}

// HasColorGrade reports whether the snapshot froze the given color grade.
func (s Snapshot) HasColorGrade(g catalog.ColorGrade) bool {
	for _, c := range s.ColorGrades {
		if c == g {
			return true
		}
	}
	return false
}

// HasResource reports whether the snapshot froze the given resource.
func (s Snapshot) HasResource(r catalog.Resource) bool {
	for _, res := range s.Resources {
		if res.Kind == r.Kind && res.ID == r.ID {
			return true
		}
	}
	return false
}

// HasScreeningPoint reports whether the snapshot froze the given point.
func (s Snapshot) HasScreeningPoint(p string) bool {
	for _, sp := range s.ScreeningPoints {
		if sp == p {
			return true
		}
	}
	return false
}
