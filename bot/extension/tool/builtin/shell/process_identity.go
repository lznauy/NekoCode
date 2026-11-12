package shell

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"time"

	"nekocode/bot/extension/tool/runtime/sandbox"
)

// Exact launch identity is intentionally conservative: no command parsing,
// whitespace rewriting or matching across owners, directories or privileges.
// Hash inherited environment without retaining or displaying its contents.
func (req startRequest) identity() string {
	env := os.Environ()
	sort.Strings(env)
	payload := struct {
		Owner, Command, Directory string
		Host                      bool
		Profile                   sandbox.Profile
		Timeout                   time.Duration
		Env                       []string
	}{req.owner, req.command, req.directory, req.host, req.profile, req.timeout, env}
	// Go-syntax formatting preserves raw string bytes and nil versus empty
	// slices, including the security-significant OutboundDomains distinction.
	digest := sha256.New()
	fmt.Fprintf(digest, "%#v", payload)
	return fmt.Sprintf("%x", digest.Sum(nil))
}
