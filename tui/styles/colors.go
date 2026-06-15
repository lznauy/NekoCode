// 色彩体系：深夜书房主题 — teal 点缀（#4ec9b0）、User 暗金（#c9a96e）、
// 文字灰阶（#a0/#80/#66）、边框隐入背景（#333333）、猫眼冷蓝（#7ec8e3）。
// Styles struct + DefaultStyles() + 包级便捷变量。
package styles

import (
	"charm.land/lipgloss/v2"
)

// SubColors are the exclusive palette for sub-agent color assignments.
// Index 0-7, each sub-agent gets a unique color during its lifetime.
var SubColors = [8]string{
	"#e57373", "#81c784", "#64b5f6", "#ffb74d",
	"#ba68c8", "#4dd0e1", "#fff176", "#f06292",
}

// Exported color hex values for direct use by other packages.
const (
	Primary   = "#4ec9b0"
	Yellow    = "#c9a96e"
	Red       = "#e06c75"
	Blue      = "#7a8ba0"
	DiffGreen = "#98c379"
	DiffDelBg  = "#3d2020"
	DiffAddBg  = "#1e3024"
	BtnYesBg   = "#1e3024"
	BtnNoBg    = "#3d2020"
	BtnNoFg    = "#e06c75"
)

const (
	fgText   = "#a0a0a0"
	fgMuted  = "#808080"
	fgSubtle = "#666666"
	fgBorder = "#333333"
	teal     = Primary
	blueInt  = Blue
	redInt   = Red
	yellow   = Yellow
	catBody  = "#4ec9b0"
	catEye   = "#7ec8e3"
)

type Styles struct {
	Base      lipgloss.Style
	Muted     lipgloss.Style
	Subtle    lipgloss.Style
	Primary   lipgloss.Style
	Teal      lipgloss.Style
	Blue      lipgloss.Style
	Red       lipgloss.Style
	Yellow    lipgloss.Style
	Border    lipgloss.Style
	CatBody   lipgloss.Style
	CatEye    lipgloss.Style
	Scrollbar struct {
		Thumb lipgloss.Style
		Track lipgloss.Style
	}
}

func DefaultStyles() Styles {
	s := Styles{
		Base:    lipgloss.NewStyle().Foreground(lipgloss.Color(fgText)),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color(fgMuted)),
		Subtle:  lipgloss.NewStyle().Foreground(lipgloss.Color(fgSubtle)),
		Primary: lipgloss.NewStyle().Foreground(lipgloss.Color(teal)),
		Teal:    lipgloss.NewStyle().Foreground(lipgloss.Color(teal)),
		Blue:    lipgloss.NewStyle().Foreground(lipgloss.Color(blueInt)),
		Red:     lipgloss.NewStyle().Foreground(lipgloss.Color(redInt)),
		Yellow:  lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)),
		Border:  lipgloss.NewStyle().Foreground(lipgloss.Color(fgBorder)),
		CatBody: lipgloss.NewStyle().Foreground(lipgloss.Color(catBody)),
		CatEye:  lipgloss.NewStyle().Foreground(lipgloss.Color(catEye)),
	}

	s.Scrollbar.Thumb = lipgloss.NewStyle().Foreground(lipgloss.Color(fgMuted))
	s.Scrollbar.Track = lipgloss.NewStyle().Foreground(lipgloss.Color(fgBorder))

	return s
}

var defaultStyles = DefaultStyles()

var (
	MutedStyle   = defaultStyles.Muted
	SubtleStyle  = defaultStyles.Subtle
	PrimaryStyle = defaultStyles.Primary
	TealStyle    = defaultStyles.Teal
	YellowStyle  = defaultStyles.Yellow
	BorderStyle  = defaultStyles.Border
	CatBodyStyle = defaultStyles.CatBody
	CatEyeStyle  = defaultStyles.CatEye
)

// BulletForBlock returns the bullet character and style for a content block.
// Main agent (SubID empty) uses teal; sub-agents use their assigned palette color.
func BulletForBlock(subID string, subColor int, tealStyle lipgloss.Style) (string, lipgloss.Style) {
	if subID == "" || subColor < 0 || subColor >= len(SubColors) {
		return MainBullet, tealStyle
	}
	c := lipgloss.Color(SubColors[subColor])
	return SubBullet, lipgloss.NewStyle().Foreground(c)
}
