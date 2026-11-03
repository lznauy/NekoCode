package permission

import "testing"

func TestBuiltinInspectionRulesDoNotAuthorizeMutations(t *testing.T) {
	e, err := NewEngineForWorkspace(PermissionsDecl{}, nil, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git tag -d release", "git remote add upstream https://example.test/repo", "git log --output=permissions.json", "git show --output=permissions.json", "git diff --output=permissions.json", "go env -w GOPROXY=https://example.test", "go fmt ./...", "go vet -vettool=/tmp/custom ./..."} {
		if d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk); d.Effect == EffectAllow {
			t.Errorf("mutating command classified as builtin inspection: %s", command)
		}
	}
	for _, command := range []string{"git status --short", "git log", "git diff", "git tag", "git remote -v", "go env"} {
		if d := e.Evaluate("shell", CallInfoForBash(command), EffectAsk); d.Effect != EffectAllow {
			t.Errorf("ordinary inspection should remain allowed: %s", command)
		}
	}
}
