package toolutil

import (
	"fmt"
	"strings"
)

const MaxUnderstandImages = 4

// ImageUnderstandingPaths is shared by the tool and its pre-approval gate.
func ImageUnderstandingPaths(args map[string]any) ([]string, error) {
	path, hasPath := args["path"]
	paths, hasPaths := args["paths"]
	if hasPath && hasPaths {
		return nil, fmt.Errorf("provide either path or paths, not both")
	}
	var values []any
	if hasPath {
		values = []any{path}
	} else if hasPaths {
		switch v := paths.(type) {
		case []any:
			values = v
		case []string:
			for _, p := range v {
				values = append(values, p)
			}
		default:
			return nil, fmt.Errorf("paths must be an array of image paths")
		}
	}
	if len(values) == 0 || len(values) > MaxUnderstandImages {
		return nil, fmt.Errorf("provide between 1 and %d images using path or paths", MaxUnderstandImages)
	}
	result := make([]string, len(values))
	for i, v := range values {
		p, ok := v.(string)
		if !ok || strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("image path #%d must be a non-empty string", i+1)
		}
		result[i] = strings.TrimSpace(p)
	}
	return result, nil
}
