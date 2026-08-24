package evidence

import (
	"errors"
	"fmt"

	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/task"
)

// ReadingKind identifies a physicochemical reading submitted through the
// collection ledger.
type ReadingKind string

// The five physicochemical reading kinds.
const (
	ReadingAcid       ReadingKind = "acid"
	ReadingPeroxide   ReadingKind = "peroxide"
	ReadingPolyphenol ReadingKind = "polyphenol"
	ReadingMoisture   ReadingKind = "moisture"
	ReadingFruitTemp  ReadingKind = "fruit-temp"
)

// ReadingKinds returns all reading kinds in canonical order.
func ReadingKinds() []ReadingKind {
	return []ReadingKind{
		ReadingAcid,
		ReadingPeroxide,
		ReadingPolyphenol,
		ReadingMoisture,
		ReadingFruitTemp,
	}
}

// ThresholdKind maps a reading kind to its catalogue threshold kind.
func (k ReadingKind) ThresholdKind() catalog.ThresholdKind {
	switch k {
	case ReadingAcid:
		return catalog.ThresholdAcid
	case ReadingPeroxide:
		return catalog.ThresholdPeroxide
	case ReadingPolyphenol:
		return catalog.ThresholdPolyphenol
	case ReadingMoisture:
		return catalog.ThresholdMoisture
	case ReadingFruitTemp:
		return catalog.ThresholdFruitTemp
	default:
		return ""
	}
}

// Reading is one parsed fixed-point reading bound to a task generation.
type Reading struct {
	TaskID     task.TaskID
	Generation task.Generation
	Kind       ReadingKind
	Value      FixedPoint
}

// ErrReadingOutOfRange is returned when a parsed reading violates the frozen
// threshold for its kind.
var ErrReadingOutOfRange = errors.New("readings: out of threshold range")

// ValidateAgainstThreshold checks a parsed reading against the frozen limit.
// The reading is rescaled to the limit's scale before comparison; a rescale
// overflow surfaces as ErrFixedPointOverflow.
func (r Reading) ValidateAgainstThreshold(limit catalog.FixedLimit) error {
	if !limit.Bounded() {
		return nil
	}
	scaled, err := r.Value.Rescale(limit.Scale)
	if err != nil {
		return err
	}
	if scaled.Value < limit.Min || scaled.Value > limit.Max {
		return fmt.Errorf("%w: %s=%d", ErrReadingOutOfRange, r.Kind, scaled.Value)
	}
	return nil
}

// Readings is a set of parsed readings for one task generation.
type Readings struct {
	Acid       FixedPoint
	Peroxide   FixedPoint
	Polyphenol FixedPoint
	Moisture   FixedPoint
	FruitTemp  FixedPoint
}

// For returns the fixed-point value for a reading kind.
func (rs Readings) For(k ReadingKind) (FixedPoint, bool) {
	switch k {
	case ReadingAcid:
		return rs.Acid, true
	case ReadingPeroxide:
		return rs.Peroxide, true
	case ReadingPolyphenol:
		return rs.Polyphenol, true
	case ReadingMoisture:
		return rs.Moisture, true
	case ReadingFruitTemp:
		return rs.FruitTemp, true
	default:
		return FixedPoint{}, false
	}
}
