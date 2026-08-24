package evidence

import (
	"errors"
	"testing"

	"github.com/olivepress/fruit-intake-gate/catalog"
)

func fullCoverage() Coverage {
	grades := []catalog.ColorGrade{catalog.ColorGreen, catalog.ColorTurning, catalog.ColorPurpleBlack, catalog.ColorDamaged, catalog.ColorMoldy}
	seals := []string{"s1", "s2"}
	total := map[string]int{"s1": 100, "s2": 100}
	cells := []MaturityCell{
		{TaskID: "t1", CrateSeal: "s1", ColorGrade: catalog.ColorGreen, Count: 60},
		{TaskID: "t1", CrateSeal: "s1", ColorGrade: catalog.ColorTurning, Count: 20},
		{TaskID: "t1", CrateSeal: "s1", ColorGrade: catalog.ColorPurpleBlack, Count: 15},
		{TaskID: "t1", CrateSeal: "s1", ColorGrade: catalog.ColorDamaged, Count: 4},
		{TaskID: "t1", CrateSeal: "s1", ColorGrade: catalog.ColorMoldy, Count: 1},
		{TaskID: "t1", CrateSeal: "s2", ColorGrade: catalog.ColorGreen, Count: 65},
		{TaskID: "t1", CrateSeal: "s2", ColorGrade: catalog.ColorTurning, Count: 18},
		{TaskID: "t1", CrateSeal: "s2", ColorGrade: catalog.ColorPurpleBlack, Count: 12},
		{TaskID: "t1", CrateSeal: "s2", ColorGrade: catalog.ColorDamaged, Count: 3},
		{TaskID: "t1", CrateSeal: "s2", ColorGrade: catalog.ColorMoldy, Count: 2},
	}
	return Coverage{CrateSeals: seals, ColorGrades: grades, Total: total, Cells: cells}
}

func TestValidateCoverageAccepts(t *testing.T) {
	if err := ValidateCoverage(fullCoverage()); err != nil {
		t.Fatalf("full coverage rejected: %v", err)
	}
}

func TestValidateCoverageRejectsMissingGrade(t *testing.T) {
	cov := fullCoverage()
	cov.Cells = cov.Cells[:len(cov.Cells)-1] // drop one cell, missing a grade
	if err := ValidateCoverage(cov); !errors.Is(err, ErrMissingColorGrade) {
		t.Fatalf("want ErrMissingColorGrade, got %v", err)
	}
}

func TestValidateCoverageRejectsExtraGrade(t *testing.T) {
	cov := fullCoverage()
	cov.Cells = append(cov.Cells, MaturityCell{CrateSeal: "s1", ColorGrade: "extra", Count: 1})
	if err := ValidateCoverage(cov); !errors.Is(err, ErrExtraColorGrade) {
		t.Fatalf("want ErrExtraColorGrade, got %v", err)
	}
}

func TestValidateCoverageRejectsNegativeCount(t *testing.T) {
	cov := fullCoverage()
	cov.Cells[0].Count = -5
	if err := ValidateCoverage(cov); !errors.Is(err, ErrNegativeCount) {
		t.Fatalf("want ErrNegativeCount, got %v", err)
	}
}

func TestValidateCoverageRejectsNonConserved(t *testing.T) {
	cov := fullCoverage()
	cov.Cells[0].Count = 61 // total becomes 101 != 100
	if err := ValidateCoverage(cov); !errors.Is(err, ErrCountNotConserved) {
		t.Fatalf("want ErrCountNotConserved, got %v", err)
	}
}
