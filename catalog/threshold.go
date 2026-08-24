package catalog

import "fmt"

// ThresholdKind identifies a cold-press physicochemical threshold.
type ThresholdKind string

// The five physicochemical threshold kinds measured on fresh olives before
// first pressing.
const (
	ThresholdAcid       ThresholdKind = "acid"
	ThresholdPeroxide   ThresholdKind = "peroxide"
	ThresholdPolyphenol ThresholdKind = "polyphenol"
	ThresholdMoisture   ThresholdKind = "moisture"
	ThresholdFruitTemp  ThresholdKind = "fruit-temp"
)

// ThresholdKinds returns all threshold kinds in canonical order. The order is
// fixed so rule digests and serialized snapshots stay deterministic.
func ThresholdKinds() []ThresholdKind {
	return []ThresholdKind{
		ThresholdAcid,
		ThresholdPeroxide,
		ThresholdPolyphenol,
		ThresholdMoisture,
		ThresholdFruitTemp,
	}
}

// FixedLimit is a fixed-scale integer bound for one reading kind. Min and Max
// are inclusive limits expressed in 10^Scale units; a zero-value bound with
// both Min and Max equal to zero means "unbounded".
type FixedLimit struct {
	Scale int   `json:"scale"`
	Min   int64 `json:"min"`
	Max   int64 `json:"max"`
}

// Bounded reports whether the limit actually constrains a reading.
func (l FixedLimit) Bounded() bool {
	return l.Scale > 0 && (l.Min != 0 || l.Max != 0)
}

// Thresholds is the frozen set of cold-press physicochemical thresholds.
type Thresholds struct {
	Acid       FixedLimit `json:"acid"`
	Peroxide   FixedLimit `json:"peroxide"`
	Polyphenol FixedLimit `json:"polyphenol"`
	Moisture   FixedLimit `json:"moisture"`
	FruitTemp  FixedLimit `json:"fruit_temp"`
}

// Limit returns the fixed limit for the given threshold kind.
func (t Thresholds) Limit(k ThresholdKind) (FixedLimit, bool) {
	switch k {
	case ThresholdAcid:
		return t.Acid, true
	case ThresholdPeroxide:
		return t.Peroxide, true
	case ThresholdPolyphenol:
		return t.Polyphenol, true
	case ThresholdMoisture:
		return t.Moisture, true
	case ThresholdFruitTemp:
		return t.FruitTemp, true
	default:
		return FixedLimit{}, false
	}
}

// Empty reports whether no threshold limit is configured.
func (t Thresholds) Empty() bool {
	return !t.Acid.Bounded() && !t.Peroxide.Bounded() &&
		!t.Polyphenol.Bounded() && !t.Moisture.Bounded() &&
		!t.FruitTemp.Bounded()
}

// serialize renders thresholds in canonical order for digest computation.
func (t Thresholds) serialize() string {
	var b []byte
	for _, k := range ThresholdKinds() {
		l, ok := t.Limit(k)
		if !ok {
			continue
		}
		b = fmt.Appendf(b, "t:%s:%d:%d:%d;", k, l.Scale, l.Min, l.Max)
	}
	return string(b)
}
