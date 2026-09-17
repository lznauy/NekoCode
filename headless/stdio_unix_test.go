//go:build unix

package headless

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPollableFileRestoresSharedFlags(t *testing.T) {
	for _, nonblocking := range []bool{false, true} {
		in, out, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		original := in.Fd()
		if err := unix.SetNonblock(int(original), nonblocking); err != nil {
			t.Fatal(err)
		}
		before, err := unix.FcntlInt(original, unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		copy, restore, err := pollableFile(in)
		if err != nil {
			t.Fatal(err)
		}
		_ = copy.Close()
		restore()
		after, err := unix.FcntlInt(original, unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		_ = in.Close()
		_ = out.Close()
		if before != after {
			t.Fatalf("shared flags changed: before=%x after=%x", before, after)
		}
	}
}
