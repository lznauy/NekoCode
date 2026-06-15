// box-drawing 字符集常量：竖线、横线、猫脸图标等 UI 框架元素。
package styles

import (
	"os"
	"strings"

	runewidth "github.com/mattn/go-runewidth"
)

// Box-drawing character set. Initialized by init() based on terminal capabilities.
var (
	Vertical    = "│"
	Horizontal  = "─"
	HeavyVert   = "┃"
)

// Agent bullets for tool attribution in activity/changes sections.
var (
	MainBullet = "ฅ"              // 猫爪 — main agent tools
	SubBullet  = "೬"              // 猫屎 — sub-agent tools
	MainCat    = "₍ᐢ ᗒ.ᗕ ᐢ₎"     // main agent cat face
	CatLeft    = "₍ᐢ"          // left ear/paw
	CatLEye    = "ᗒ"           // left eye
	CatNose    = "."            // nose
	CatREye    = "ᗕ"           // right eye
	CatRight   = " ᐢ₎"         // right paw/ear
)

func init() {
	runewidth.DefaultCondition.EastAsianWidth = true

	if !supportsUnicode() {
		Vertical = "|"
		Horizontal = "-"
		HeavyVert = "|"
		MainBullet = "*"
		SubBullet = "*"
		MainCat = "(=^.^=)"
	}
}

func supportsUnicode() bool {
	for _, env := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		v := strings.ToUpper(os.Getenv(env))
		if strings.Contains(v, "UTF-8") || strings.Contains(v, "UTF8") {
			return true
		}
	}
	switch os.Getenv("TERM") {
	case "dumb", "vt100", "vt52", "linux":
		return false
	}
	return true
}
