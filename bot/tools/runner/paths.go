package runner

import (
	"nekocode/bot/tools/core"
	"nekocode/bot/tools/editdsl"
	"nekocode/bot/tools/pathutil"
)

func confirmArgs(name string, args map[string]any) map[string]any {
	if name == "edit" {
		paths := editdsl.ExtractPathsFromPatch(args["patch"])
		if len(paths) > 0 {
			out := make(map[string]any, len(args)+1)
			for k, v := range args {
				out[k] = v
			}
			out["path"] = paths[0]
			return out
		}
	}
	return args
}

func toolPaths(tc core.ToolCallItem) []string {
	if tc.Name == "edit" {
		return editdsl.ExtractPathsFromPatch(tc.Args["patch"])
	}
	if p, ok := tc.Args["path"].(string); ok && p != "" {
		return []string{p}
	}
	return nil
}

func validatePath(path string) (string, error) {
	return pathutil.ValidatePath(path)
}
