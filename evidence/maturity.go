package evidence

import (
	"errors"
	"sort"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

// MaturityCell is a single maturity coverage cell: the integer count of fruit
// of one color grade observed in one crate seal.
type MaturityCell struct {
	TaskID     task.TaskID
	Generation task.Generation
	CrateSeal  string
	ColorGrade catalog.ColorGrade
	Count      int
}

// Sentinel errors for maturity coverage validation.
var (
	ErrMissingColorGrade = errors.New("maturity: missing color grade")
	ErrExtraColorGrade   = errors.New("maturity: extra color grade")
	ErrMissingCrate      = errors.New("maturity: missing crate seal")
	ErrExtraCrate        = errors.New("maturity: extra crate seal")
	ErrNegativeCount     = errors.New("maturity: negative count")
	ErrCountNotConserved = errors.New("maturity: count not conserved")
)

// Coverage is a full maturity coverage grid over crate seals and color grades.
type Coverage struct {
	CrateSeals  []string
	ColorGrades []catalog.ColorGrade
	Total       map[string]int // crate seal -> expected sample grain total
	Cells       []MaturityCell
}

// ValidateCoverage checks that the submitted cells form a complete,
// conserved grid over the locked color grades and crate seals. Every locked
// color grade and crate must be present exactly, counts must be non-negative,
// and each crate's counts must sum to its expected sample grain total.
func ValidateCoverage(cov Coverage) error {
	grades := append([]catalog.ColorGrade(nil), cov.ColorGrades...)
	sort.Slice(grades, func(i, j int) bool { return grades[i] < grades[j] })
	seals := append([]string(nil), cov.CrateSeals...)
	sort.Strings(seals)

	expected := make(map[string]int, len(seals))
	for _, s := range seals {
		expected[s] = cov.Total[s]
	}
	lockedGrades := make(map[catalog.ColorGrade]bool, len(grades))
	for _, g := range grades {
		lockedGrades[g] = true
	}

	type key struct {
		seal  string
		grade catalog.ColorGrade
	}
	seen := make(map[key]int, len(cov.Cells))
	for _, c := range cov.Cells {
		if c.Count < 0 {
			return ErrNegativeCount
		}
		if _, ok := expected[c.CrateSeal]; !ok {
			return ErrExtraCrate
		}
		if !lockedGrades[c.ColorGrade] {
			return ErrExtraColorGrade
		}
		k := key{seal: c.CrateSeal, grade: c.ColorGrade}
		if _, dup := seen[k]; dup {
			return ErrExtraColorGrade
		}
		seen[k] = c.Count
	}

	// Every crate must have a count for every locked color grade.
	for _, s := range seals {
		sum := 0
		for _, g := range grades {
			n, ok := seen[key{seal: s, grade: g}]
			if !ok {
				return ErrMissingColorGrade
			}
			sum += n
		}
		if sum != expected[s] {
			return ErrCountNotConserved
		}
	}
	return nil
}

// GradeTotal sums the counts of a single color grade across all crates.
func (cov Coverage) GradeTotal(g catalog.ColorGrade) int {
	total := 0
	for _, c := range cov.Cells {
		if c.ColorGrade == g {
			total += c.Count
		}
	}
	return total
}
