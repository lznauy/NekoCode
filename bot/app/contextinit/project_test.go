package contextinit

import (
	"errors"
	"testing"

	graphpkg "nekocode/bot/index/graph"
	"nekocode/bot/index/service"
)

type fakeTarget struct {
	added  []string
	budget int
}

func (t *fakeTarget) Add(role, content string, source ...string) {
	t.added = append(t.added, role+":"+content)
}

func (t *fakeTarget) SetContextWindow(budget int) {
	t.budget = budget
}

type fakeIndex struct {
	initErr error
	graph   *graphpkg.Graph
}

func (m *fakeIndex) Init() error            { return m.initErr }
func (m *fakeIndex) Graph() *graphpkg.Graph { return m.graph }

func TestApplyProjectContextAndIndexEmptyCWD(t *testing.T) {
	target := &fakeTarget{}
	got := ApplyProjectContextAndIndex(target, Options{ContextWindow: 100})
	if got.ProjectContext != "" || got.IndexManager != nil {
		t.Fatalf("unexpected result: %+v", got)
	}
	if target.budget != 100 {
		t.Fatalf("budget not applied: %d", target.budget)
	}
}

func TestApplyProjectContextAndIndexAddsProjectText(t *testing.T) {
	target := &fakeTarget{}
	got := ApplyProjectContextAndIndex(target, Options{
		CWD:           "/repo",
		ContextWindow: 200,
		LoadProjectText: func(string) string {
			return "project rules"
		},
		NewIndexManager: func(string) (*service.Manager, error) {
			return nil, errors.New("skip index")
		},
	})
	if got.ProjectContext != "project rules" {
		t.Fatalf("project context = %q", got.ProjectContext)
	}
	if len(target.added) != 1 || target.added[0] != "system:project rules" {
		t.Fatalf("added = %+v", target.added)
	}
	if target.budget != 200 {
		t.Fatalf("budget = %d", target.budget)
	}
}

func TestInjectSkeleton(t *testing.T) {
	graph := graphpkg.NewGraph()
	graph.AddNode(&graphpkg.Node{ID: 1, Name: "a.go", Kind: graphpkg.KindFile, File: "/repo/a.go"})
	target := &fakeTarget{}
	injectSkeleton(target, &fakeIndex{graph: graph}, "/repo")
	if len(target.added) != 1 {
		t.Fatalf("expected skeleton injection, got %+v", target.added)
	}
}
