package ledger

import (
	"testing"

	"github.com/olivepress/fruit-intake-gate/catalog"
)

func TestLogicalClockMonotonic(t *testing.T) {
	c := NewClock()
	var prev Tick = c.Now()
	for i := 0; i < 5; i++ {
		cur := c.Advance()
		if cur <= prev {
			t.Fatalf("clock not monotonic: %d <= %d", cur, prev)
		}
		prev = cur
	}
}

func TestLeaseActiveWindow(t *testing.T) {
	c := NewClock()
	l := NewLease(c, "t1", catalog.ResourceCrusherLine, "cl-1", 1)
	if !l.IsActive(c.Now()) {
		t.Fatal("lease should be active at start")
	}
	if l.IsActive(l.ExpireTick) {
		t.Fatal("lease should be expired at expire tick")
	}
	released := l.StartTick
	l.ReleasedTick = &released
	if l.IsActive(c.Now()) {
		t.Fatal("lease should be inactive after release")
	}
}
