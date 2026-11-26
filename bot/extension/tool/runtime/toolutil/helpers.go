package toolutil

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"

	"nekocode/bot/extension/tool/runtime/core"
)

type SafeReadOnlyTool struct{}

func (t *SafeReadOnlyTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeParallel
}

type SequentialSafeTool struct{}

func (t *SequentialSafeTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

type WriteModeTool struct{}

func (t *WriteModeTool) ExecutionMode(map[string]any) core.ExecutionMode {
	return core.ModeSequential
}

func RequireStringArg(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("missing %s parameter", key)
	}
	return v, nil
}

// StringArg requires a string to be supplied, but permits explicit empty text.
func StringArg(args map[string]any, key string) (string, error) {
	if _, ok := args[key]; !ok {
		return "", fmt.Errorf("missing %s parameter", key)
	}
	v, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string (use an explicit empty string for empty content)", key)
	}
	return v, nil
}

// FloatArgValue reads numeric inputs for tools accepting fractional values or
// explicitly clamping a value before conversion. Integer identifiers stay exact
// until an integer consumer calls RequireIntArg.
func FloatArgValue(value any) (float64, bool) {
	switch n := value.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case int:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func RequireIntArg(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing %s parameter", key)
	}
	if n, ok := v.(int); ok {
		return n, nil
	}
	if number, ok := v.(json.Number); ok {
		if n, err := strconv.ParseInt(string(number), 10, strconv.IntSize); err == nil {
			return int(n), nil
		}
		// JSON permits integral decimal and exponent forms. Reject fractions
		// and out-of-range integers without rounding through float64.
		if f, err := number.Float64(); err == nil && math.Abs(f) <= math.Ldexp(1, strconv.IntSize-1) {
			if rational, ok := new(big.Rat).SetString(string(number)); ok && rational.IsInt() && rational.Num().IsInt64() {
				n := rational.Num().Int64()
				if int64(int(n)) == n {
					return int(n), nil
				}
			}
		}
	}
	f, ok := v.(float64)
	// The upper bound is exclusive: float64(maxInt) rounds up on 64-bit
	// systems, and converting that rounded value would overflow.
	bound := math.Ldexp(1, strconv.IntSize-1)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f || f < -bound || f >= bound {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, -int(^uint(0)>>1)-1, int(^uint(0)>>1))
	}
	return int(f), nil
}

func OptStringArg(args map[string]any, key, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

func OptIntArg(args map[string]any, key string, def int) (int, error) {
	if _, supplied := args[key]; !supplied {
		return def, nil
	}
	return RequireIntArg(args, key)
}

func ClampIntArg(args map[string]any, key string, def, min, max int) (int, error) {
	n, err := OptIntArg(args, key, def)
	if err != nil {
		return 0, err
	}
	if n < min {
		return min, nil
	}
	if n > max {
		return max, nil
	}
	return n, nil
}

func GetFileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode()
	}
	return 0644
}
