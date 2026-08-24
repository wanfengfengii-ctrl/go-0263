package evidence

import (
	"errors"
	"testing"
)

func TestParseFixed(t *testing.T) {
	cases := []struct {
		in    string
		scale int
		want  int64
	}{
		{"0.42", 2, 42},
		{"1.5", 2, 150},
		{"3", 2, 300},
		{"0", 2, 0},
		{"-0.5", 2, -50},
		{"+2.50", 2, 250},
	}
	for _, c := range cases {
		got, err := ParseFixed(c.in, c.scale)
		if err != nil {
			t.Fatalf("ParseFixed(%q, %d): %v", c.in, c.scale, err)
		}
		if got.Value != c.want {
			t.Fatalf("ParseFixed(%q, %d) = %d, want %d", c.in, c.scale, got.Value, c.want)
		}
	}
}

func TestParseFixedRejects(t *testing.T) {
	cases := []struct {
		in    string
		scale int
		want  error
	}{
		{"", 2, ErrFixedPointInvalid},
		{"abc", 2, ErrFixedPointInvalid},
		{"1.234", 2, ErrFixedPointInvalid}, // too many fractional digits
		{"1.2.3", 2, ErrFixedPointInvalid},
		{"99999999999999999999", 0, ErrFixedPointLength}, // 20 digits
		{"1", -1, ErrFixedPointScale},
	}
	for _, c := range cases {
		_, err := ParseFixed(c.in, c.scale)
		if !errors.Is(err, c.want) {
			t.Fatalf("ParseFixed(%q, %d) = %v, want %v", c.in, c.scale, err, c.want)
		}
	}
}

func TestFixedPointArithmetic(t *testing.T) {
	a, _ := ParseFixed("1.5", 2)
	b, _ := ParseFixed("0.25", 2)

	sum, err := a.Add(b)
	if err != nil || sum.Value != 175 {
		t.Fatalf("Add = %v, %v; want 175", sum, err)
	}
	diff, err := a.Sub(b)
	if err != nil || diff.Value != 125 {
		t.Fatalf("Sub = %v, %v; want 125", diff, err)
	}
	prod, err := a.Mul(b)
	if err != nil || prod.Value != 3750 {
		t.Fatalf("Mul = %v, %v; want 3750", prod, err)
	}
	quot, err := a.Div(b, 2)
	if err != nil || quot.Value != 600 {
		t.Fatalf("Div = %v, %v; want 600", quot, err)
	}
}

func TestFixedPointDivideByZero(t *testing.T) {
	a, _ := ParseFixed("1.0", 2)
	zero := FixedPoint{Value: 0, Scale: 2}
	if _, err := a.Div(zero, 2); !errors.Is(err, ErrFixedPointDivideByZero) {
		t.Fatalf("expected ErrFixedPointDivideByZero, got %v", err)
	}
}

func TestFixedPointRescaleOverflow(t *testing.T) {
	big := FixedPoint{Value: 1 << 62, Scale: 0}
	if _, err := big.Rescale(2); !errors.Is(err, ErrFixedPointOverflow) {
		t.Fatalf("expected ErrFixedPointOverflow, got %v", err)
	}
}
