package media

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"nekocode/bot/extension/tool/runtime/toolutil"
)

func validateGenerationArgs(prompt string, args map[string]any) error {
	if strings.TrimSpace(prompt) == "" || utf8.RuneCountInString(prompt) > 800 {
		return fmt.Errorf("prompt must contain 1–800 characters with non-whitespace text; no generation was submitted")
	}
	_, widthSet := args["width"]
	_, heightSet := args["height"]
	if widthSet != heightSet {
		return fmt.Errorf("width and height must be supplied together, or omit both for 1328×1328; no generation was submitted")
	}
	if widthSet {
		width, err := toolutil.RequireIntArg(args, "width")
		if err != nil {
			return err
		}
		height, err := toolutil.RequireIntArg(args, "height")
		if err != nil {
			return err
		}
		if err := validateGeneratedDimensions(width, height); err != nil {
			return err
		}
	}
	seed, err := toolutil.OptIntArg(args, "seed", -1)
	if err != nil {
		return err
	}
	if seed < -1 {
		return fmt.Errorf("seed must be -1 or a non-negative integer; no generation was submitted")
	}
	return nil
}
