package processing

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/styles"
)

func TestWrapTailMatchesFullWrapping(t *testing.T) {
	texts := []string{strings.Repeat("a", 20) + "\xff", "", "\n\n", "hello\n\n", strings.Repeat("a", 300), "a\n\n---\n b \n", strings.Repeat("中文🙂é", 80) + "\n末尾\n\n"}
	rng := rand.New(rand.NewSource(1))
	alphabet := []rune("ab中文🙂 \n\t")
	for j := 0; j < 100; j++ {
		var s strings.Builder
		for k := 0; k < 200; k++ {
			s.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		texts = append(texts, s.String())
	}
	sty := styles.DefaultStyles()
	for _, text := range texts {
		for _, width := range []int{0, 1, 7, 40, 100} {
			for _, lines := range []int{1, 3, 6} {
				for _, skip := range []bool{false, true} {
					want := RenderFixed(WrapPlain(text, width), lines, skip, sty.Base)
					got := RenderFixed(wrapTail(text, width, lines), lines, skip, sty.Base)
					if got != want {
						t.Fatalf("tail mismatch width=%d lines=%d skip=%v text=%q\ngot=%q\nwant=%q", width, lines, skip, text, got, want)
					}
				}
			}
		}
	}
}

func TestTextCacheTracksUpdatesResetsAndWidth(t *testing.T) {
	sty := styles.DefaultStyles()
	p := NewProcessingItem(&sty)
	p.AppendStreamText("old output")
	p.AppendThinkingText("old thinking")
	p.Render(100)
	oldOutput, oldThinking := p.cachedOutput, p.cachedThinking
	p.SetSpinnerView(".")
	p.Render(100)
	if p.cachedOutput != oldOutput || p.cachedThinking != oldThinking {
		t.Fatal("status update changed cached text")
	}
	p.DiscardStreamText()
	p.AppendStreamText("new output") // same byte length as the discarded text
	p.Render(100)
	rendered := ansi.Strip(p.cachedOutput)
	if !strings.Contains(rendered, "new output") || strings.Contains(rendered, "old output") {
		t.Fatal("reset reused stale output")
	}
	p.AppendThinkingText(" continued")
	p.Render(100)
	if !strings.Contains(ansi.Strip(p.cachedThinking), "continued") {
		t.Fatal("thinking append was not rendered")
	}
	p.Render(60)
	if p.cachedOutputW != styles.MessageWidth(60)-4 || p.cachedThinkingW != p.cachedOutputW {
		t.Fatal("resize reused stale text width")
	}
	p.AddToolBlock(block.ContentBlock{Type: block.BlockTool, ToolName: "read"})
	p.Render(60)
	if p.cachedOutput != "" {
		t.Fatal("flushed output remained visible")
	}
	p.Clear()
	p.Render(60)
	if p.cachedOutput != "" || p.cachedThinking != "" {
		t.Fatal("clear retained cached text")
	}
}
