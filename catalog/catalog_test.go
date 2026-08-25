package catalog

import (
	"errors"
	"testing"
	"time"
)

func TestComputeRuleDigestDeterministic(t *testing.T) {
	grades := []ColorGrade{ColorGreen, ColorTurning, ColorPurpleBlack}
	resources := []Resource{
		{Kind: ResourceCrusherLine, ID: "cl-1"},
		{Kind: ResourceInertWindow, ID: "iw-9"},
	}
	reviewers := []ReviewerID{"rev-b", "rev-a"}

	d1 := ComputeRuleDigest(grades, resources, reviewers)
	d2 := ComputeRuleDigest(grades, resources, reviewers)
	if d1 != d2 {
		t.Fatalf("digest not deterministic: %q vs %q", d1, d2)
	}
	if d1 == "" {
		t.Fatal("digest is empty")
	}
}

func TestValidateLockInput(t *testing.T) {
	harvest := HarvestPeriod{
		Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC),
	}
	rule := Rule{
		ID:          "r1",
		Digest:      "abc",
		ColorGrades: ValidColorGrades,
		ReviewerIDs: []ReviewerID{"rev-a", "rev-b"},
	}
	plot := Plot{
		ID:            "p1",
		CultivarID:    "picual",
		HarvestPeriod: harvest,
		RuleDigest:    "abc",
	}

	t.Run("valid", func(t *testing.T) {
		if err := ValidateLockInput(plot, rule, "picual", harvest.Start.Add(24*time.Hour), []ReviewerID{"rev-a", "rev-b"}); err != nil {
			t.Fatalf("expected valid lock, got %v", err)
		}
	})

	t.Run("stale rule digest", func(t *testing.T) {
		stale := rule
		stale.Digest = "different"
		err := ValidateLockInput(plot, stale, "picual", harvest.Start, []ReviewerID{"rev-a"})
		if !errors.Is(err, ErrStaleRuleDigest) {
			t.Fatalf("expected ErrStaleRuleDigest, got %v", err)
		}
	})

	t.Run("cultivar mismatch", func(t *testing.T) {
		err := ValidateLockInput(plot, rule, "arbequina", harvest.Start, []ReviewerID{"rev-a"})
		if !errors.Is(err, ErrCultivarMismatch) {
			t.Fatalf("expected ErrCultivarMismatch, got %v", err)
		}
	})

	t.Run("harvest window miss", func(t *testing.T) {
		before := harvest.Start.Add(-1 * time.Hour)
		err := ValidateLockInput(plot, rule, "picual", before, []ReviewerID{"rev-a"})
		if !errors.Is(err, ErrHarvestWindowMiss) {
			t.Fatalf("expected ErrHarvestWindowMiss, got %v", err)
		}
	})

	t.Run("unqualified reviewer", func(t *testing.T) {
		err := ValidateLockInput(plot, rule, "picual", harvest.Start, []ReviewerID{"rev-x"})
		if !errors.Is(err, ErrReviewerNotQualified) {
			t.Fatalf("expected ErrReviewerNotQualified, got %v", err)
		}
	})
}
