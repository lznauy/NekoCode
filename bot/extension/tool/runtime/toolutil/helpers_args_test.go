package toolutil

import (
	"math"
	"strconv"
	"testing"
)

func TestRequireIntArgRejectsInvalidNumbers(t *testing.T) {
	for _, value := range []any{1.9, -1.9, math.NaN(), math.Inf(1), math.Inf(-1), math.Ldexp(1, strconv.IntSize-1), -math.Ldexp(1, strconv.IntSize), "2", nil} {
		if got, err := RequireIntArg(map[string]any{"line": value}, "line"); err == nil {
			t.Errorf("accepted %v (%T) as %d", value, value, got)
		}
	}
}

func TestRequireIntArgAcceptsWireAndNativeIntegers(t *testing.T) {
	for _, value := range []any{float64(2), int(2)} {
		if got, err := RequireIntArg(map[string]any{"line": value}, "line"); err != nil || got != 2 {
			t.Errorf("value=%v: got %d, %v", value, got, err)
		}
	}
}

func TestOptionalIntegersOnlyDefaultWhenOmitted(t *testing.T) {
	for _, value := range []any{1.9, "2", nil, math.Inf(1)} {
		args := map[string]any{"count": value}
		if _, err := OptIntArg(args, "count", 3); err == nil {
			t.Errorf("invalid optional count accepted: %v", value)
		}
		if _, err := ClampIntArg(args, "count", 3, 1, 6); err == nil {
			t.Errorf("invalid clamped count accepted: %v", value)
		}
	}
	if got, err := OptIntArg(nil, "count", 3); err != nil || got != 3 {
		t.Fatalf("default broken: %d %v", got, err)
	}
	if got, err := ClampIntArg(map[string]any{"count": float64(9)}, "count", 3, 1, 6); err != nil || got != 6 {
		t.Fatalf("clamping broken: %d %v", got, err)
	}
}
