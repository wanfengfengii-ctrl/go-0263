package catalog

import (
	"sort"
	"time"
)

// Catalog is the in-memory olive grove and cold-press rule catalogue. A
// running service seeds it at startup; the HTTP lock operation reads from it
// to validate cultivars, harvest windows, rule digests and reviewers.
type Catalog struct {
	plots map[PlotID]Plot
	rules map[RuleDigest]Rule
}

// NewCatalog returns an empty catalogue.
func NewCatalog() *Catalog {
	return &Catalog{
		plots: make(map[PlotID]Plot),
		rules: make(map[RuleDigest]Rule),
	}
}

// RegisterPlot adds a grove plot, replacing any existing plot with the same ID.
func (c *Catalog) RegisterPlot(p Plot) {
	c.plots[p.ID] = p
}

// RegisterRule adds a rule keyed by its digest, recomputing the digest if it
// is empty so callers can register a rule without hand-rolling the hash.
func (c *Catalog) RegisterRule(r Rule) Rule {
	if r.Digest == "" {
		r.Digest = r.ComputeDigest()
	}
	c.rules[r.Digest] = r
	return r
}

// Plot returns the plot with the given id.
func (c *Catalog) Plot(id PlotID) (Plot, bool) {
	p, ok := c.plots[id]
	return p, ok
}

// Rule returns the rule with the given digest.
func (c *Catalog) Rule(d RuleDigest) (Rule, bool) {
	r, ok := c.rules[d]
	return r, ok
}

// Plots returns all plots sorted by id for deterministic serialization.
func (c *Catalog) Plots() []Plot {
	out := make([]Plot, 0, len(c.plots))
	for _, p := range c.plots {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Rules returns all rules sorted by digest.
func (c *Catalog) Rules() []Rule {
	out := make([]Rule, 0, len(c.rules))
	for _, r := range c.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Digest < out[j].Digest })
	return out
}

// SeedCatalog builds the fictional Picual grove catalogue used by tests and
// the smoke script. It defines one harvest window, one cold-press rule and a
// roster of qualified reviewers.
func SeedCatalog() *Catalog {
	c := NewCatalog()

	harvest := HarvestPeriod{
		Start: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC),
	}

	resources := []Resource{
		{Kind: ResourceCrusherLine, ID: "cl-1"},
		{Kind: ResourceInertWindow, ID: "iw-1"},
		{Kind: ResourceTestHole, ID: "th-1"},
		{Kind: ResourceTestHole, ID: "th-2"},
	}

	thresholds := Thresholds{
		Acid:       FixedLimit{Scale: 2, Min: 0, Max: 80},   // oleic acid <= 0.80 %
		Peroxide:   FixedLimit{Scale: 2, Min: 0, Max: 2000}, // peroxide <= 20.00 meq/kg
		Polyphenol: FixedLimit{Scale: 0, Min: 150, Max: 0},  // polyphenols >= 150 mg/kg
		Moisture:   FixedLimit{Scale: 1, Min: 0, Max: 550},  // moisture <= 55.0 %
		FruitTemp:  FixedLimit{Scale: 1, Min: 0, Max: 350},  // fruit temp <= 35.0 C
	}

	rule := Rule{
		ID:              "picual-2026",
		ColorGrades:     ValidColorGrades,
		Resources:       resources,
		Thresholds:      thresholds,
		ScreeningPoints: []string{"sp-1", "sp-2"},
		ReviewerIDs:     []ReviewerID{"rev-a", "rev-b", "rev-c"},
	}
	rule = c.RegisterRule(rule)

	c.RegisterPlot(Plot{
		ID:            "plot-picual-1",
		CultivarID:    "picual",
		HarvestPeriod: harvest,
		RuleDigest:    rule.Digest,
	})
	c.RegisterPlot(Plot{
		ID:            "plot-picual-2",
		CultivarID:    "picual",
		HarvestPeriod: harvest,
		RuleDigest:    rule.Digest,
	})

	return c
}
