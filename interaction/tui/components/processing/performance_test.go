package processing

import (
	"fmt"
	"nekocode/interaction/tui/styles"
	"strings"
	"testing"
)

func BenchmarkSpinnerWithLongText(b *testing.B) {
	for _, kb := range []int{64, 256, 1024} {
		b.Run(fmt.Sprintf("%d_KiB", kb), func(b *testing.B) {
			sty := styles.DefaultStyles()
			p := NewProcessingItem(&sty)
			p.AppendStreamText(strings.Repeat("output line with some text 0123456789\n", kb*1024/37))
			p.AppendThinkingText(strings.Repeat("reasoning line abcdefghijklmnopqrstuvwxyz\n", kb*1024/41))
			p.Render(100)
			b.ReportAllocs()
			b.ResetTimer()
			for j := 0; j < b.N; j++ {
				p.SetSpinnerView(".")
				p.Render(100)
			}
		})
	}
}

func BenchmarkAppendLongText(b *testing.B) {
	sty := styles.DefaultStyles()
	p := NewProcessingItem(&sty)
	text := strings.Repeat("a line of output and reasoning\n", 32768)
	p.AppendStreamText(text)
	p.AppendThinkingText(text)
	p.Render(100)
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		p.AppendStreamText("delta\n")
		p.AppendThinkingText("delta\n")
		p.Render(100)
	}
}
