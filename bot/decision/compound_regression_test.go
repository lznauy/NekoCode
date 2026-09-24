package decision

import (
	"context"
	"testing"
)

func TestCompoundCommandDoesNotReuseIsolatedVerdicts(t *testing.T) {
	n := 0
	srv := riskServer(t, nil, &n)
	defer srv.Close()
	s := newScorer(JevSettings{APIKey: "test", BaseURL: srv.URL})
	s.SetRiskContext("/repo")
	s.Dangerous(context.Background(), "rm -rf build")
	s.Dangerous(context.Background(), "cd /production")
	before := n
	danger, ok := s.Dangerous(context.Background(), "cd /production && rm -rf build")
	if ok && !danger && n == before {
		t.Fatal("compound approved entirely from isolated-command caches despite changed execution directory")
	}
}
