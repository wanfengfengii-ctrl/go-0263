// Package evidence maintains the maturity and physicochemical collection
// ledger: maturity coverage cells, fixed-scale integer readings and the
// immutable append-only version chain.
package evidence

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Sentinel errors for fixed-point parsing and arithmetic.
var (
	ErrFixedPointInvalid      = errors.New("fixed point: invalid format")
	ErrFixedPointSign         = errors.New("fixed point: negative value not allowed")
	ErrFixedPointLength       = errors.New("fixed point: too many digits")
	ErrFixedPointOverflow     = errors.New("fixed point: int64 overflow")
	ErrFixedPointDivideByZero = errors.New("fixed point: divide by zero")
	ErrFixedPointScale        = errors.New("fixed point: invalid scale")
)

// maxDigits bounds the integer-digit count to guarantee int64 safety before
// any multiplication during rescaling.
const maxDigits = 18

// FixedPoint is a fixed-scale integer reading. Value is the integer value
// scaled by 10^Scale.
type FixedPoint struct {
	Value int64
	Scale int
}

// ParseFixed parses a decimal string into a fixed-point value at the given
// scale, enforcing sign, length, scale and int64-overflow constraints.
func ParseFixed(s string, scale int) (FixedPoint, error) {
	if scale < 0 {
		return FixedPoint{}, ErrFixedPointScale
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return FixedPoint{}, ErrFixedPointInvalid
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return FixedPoint{}, ErrFixedPointInvalid
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" && fracPart == "" {
		return FixedPoint{}, ErrFixedPointInvalid
	}
	if strings.Trim(intPart, "0123456789") != "" || (hasFrac && strings.Trim(fracPart, "0123456789") != "") {
		return FixedPoint{}, ErrFixedPointInvalid
	}

	// Normalize the fractional part to the requested scale.
	if len(fracPart) > scale {
		return FixedPoint{}, ErrFixedPointInvalid
	}
	fracPart = fracPart + strings.Repeat("0", scale-len(fracPart))

	digits := strings.TrimLeft(intPart, "0") + fracPart
	if digits == "" {
		digits = "0"
	}
	if len(strings.TrimLeft(intPart, "0")) > maxDigits {
		return FixedPoint{}, ErrFixedPointLength
	}

	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return FixedPoint{}, ErrFixedPointOverflow
		}
		return FixedPoint{}, ErrFixedPointInvalid
	}
	if neg {
		v = -v
	}
	return FixedPoint{Value: v, Scale: scale}, nil
}

// Rescale converts the value to a new scale, checking divide-by-zero
// (newScale < 0) and int64 overflow.
func (f FixedPoint) Rescale(newScale int) (FixedPoint, error) {
	if newScale < 0 {
		return FixedPoint{}, ErrFixedPointScale
	}
	if newScale == f.Scale {
		return f, nil
	}
	if newScale > f.Scale {
		// Multiply by 10^(newScale-f.Scale).
		mult := pow10(newScale - f.Scale)
		v, ok := mulCheck(f.Value, mult)
		if !ok {
			return FixedPoint{}, ErrFixedPointOverflow
		}
		return FixedPoint{Value: v, Scale: newScale}, nil
	}
	// Divide by 10^(f.Scale-newScale), truncating toward zero.
	div := pow10(f.Scale - newScale)
	return FixedPoint{Value: f.Value / div, Scale: newScale}, nil
}

// Add returns f + g after aligning scales, checking overflow.
func (f FixedPoint) Add(g FixedPoint) (FixedPoint, error) {
	a, b, scale, err := align(f, g)
	if err != nil {
		return FixedPoint{}, err
	}
	v, ok := addCheck(a, b)
	if !ok {
		return FixedPoint{}, ErrFixedPointOverflow
	}
	return FixedPoint{Value: v, Scale: scale}, nil
}

// Sub returns f - g after aligning scales, checking overflow.
func (f FixedPoint) Sub(g FixedPoint) (FixedPoint, error) {
	a, b, scale, err := align(f, g)
	if err != nil {
		return FixedPoint{}, err
	}
	v, ok := subCheck(a, b)
	if !ok {
		return FixedPoint{}, ErrFixedPointOverflow
	}
	return FixedPoint{Value: v, Scale: scale}, nil
}

// Mul returns f * g at the higher of the two scales, checking overflow.
func (f FixedPoint) Mul(g FixedPoint) (FixedPoint, error) {
	v, ok := mulCheck(f.Value, g.Value)
	if !ok {
		return FixedPoint{}, ErrFixedPointOverflow
	}
	scale := f.Scale
	if g.Scale > scale {
		scale = g.Scale
	}
	return FixedPoint{Value: v, Scale: scale}, nil
}

// Div returns f / g at the given result scale, checking divide-by-zero and
// overflow during rescaling of the numerator.
func (f FixedPoint) Div(g FixedPoint, resultScale int) (FixedPoint, error) {
	if g.Value == 0 {
		return FixedPoint{}, ErrFixedPointDivideByZero
	}
	if resultScale < 0 {
		return FixedPoint{}, ErrFixedPointScale
	}
	// Rescale numerator to carry enough precision, then integer-divide.
	num := f.Value
	shift := resultScale + g.Scale - f.Scale
	if shift < 0 {
		div := pow10(-shift)
		return FixedPoint{Value: num / g.Value / div, Scale: resultScale}, nil
	}
	mult := pow10(shift)
	num, ok := mulCheck(num, mult)
	if !ok {
		return FixedPoint{}, ErrFixedPointOverflow
	}
	return FixedPoint{Value: num / g.Value, Scale: resultScale}, nil
}

// align brings two fixed-point values to a common scale.
func align(f, g FixedPoint) (int64, int64, int, error) {
	if f.Scale == g.Scale {
		return f.Value, g.Value, f.Scale, nil
	}
	if f.Scale > g.Scale {
		mult := pow10(f.Scale - g.Scale)
		gv, ok := mulCheck(g.Value, mult)
		if !ok {
			return 0, 0, 0, ErrFixedPointOverflow
		}
		return f.Value, gv, f.Scale, nil
	}
	mult := pow10(g.Scale - f.Scale)
	fv, ok := mulCheck(f.Value, mult)
	if !ok {
		return 0, 0, 0, ErrFixedPointOverflow
	}
	return fv, g.Value, g.Scale, nil
}

// pow10 returns 10^n for n >= 0, saturating at math.MaxInt64 on overflow.
func pow10(n int) int64 {
	if n <= 0 {
		return 1
	}
	v := int64(1)
	for i := 0; i < n; i++ {
		if v > math.MaxInt64/10 {
			return math.MaxInt64
		}
		v *= 10
	}
	return v
}

func mulCheck(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	v := a * b
	if v/b != a {
		return 0, false
	}
	return v, true
}

func addCheck(a, b int64) (int64, bool) {
	v := a + b
	if (b > 0 && v < a) || (b < 0 && v > a) {
		return 0, false
	}
	return v, true
}

func subCheck(a, b int64) (int64, bool) {
	v := a - b
	if (b < 0 && v < a) || (b > 0 && v > a) {
		return 0, false
	}
	return v, true
}
