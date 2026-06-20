package execution

import (
	"path/filepath"
	"sort"
)

// mergeRanges inserts r into a sorted, non-overlapping slice of ranges.
func mergeRanges(ranges []lineRange, r lineRange) []lineRange {
	ranges = append(ranges, r)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })

	merged := ranges[:0]
	for _, rg := range ranges {
		if len(merged) == 0 || merged[len(merged)-1].End < rg.Start-1 {
			merged = append(merged, rg)
		} else if rg.End > merged[len(merged)-1].End {
			merged[len(merged)-1].End = rg.End
		}
	}
	return merged
}

func normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		parent := filepath.Dir(abs)
		realParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			return abs
		}
		return filepath.Join(realParent, filepath.Base(abs))
	}
	return real
}
