package tui

import "testing"

func TestFormatBriefArgsParsesJSONToolArgs(t *testing.T) {
	if got := formatBriefArgs("edit", `{"path":"/tmp/a.go","oldString":"a","newString":"b"}`); got != "/tmp/a.go" {
		t.Fatalf("edit args = %q, want path", got)
	}
	if got := formatBriefArgs("bash", `{"command":"go test ./..."}`); got != "go test ./..." {
		t.Fatalf("bash args = %q, want command", got)
	}
}

func TestFormatBriefArgsKeepsPairSyntax(t *testing.T) {
	if got := formatBriefArgs("edit", `path=/tmp/a.go,oldString=a,newString=b`); got != "/tmp/a.go" {
		t.Fatalf("edit args = %q, want path", got)
	}
}
