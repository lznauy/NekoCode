package shell

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func emptyGoTestReport(command string, output []byte) bool {
	var r emptyGoReport
	r.Write(output)
	return directGoTest(command) && r.finish()
}

func TestRealGoTestSelectionObservation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"), []byte("package probe\nimport \"testing\"\nfunc TestProbe(t *testing.T) {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"TestMissing", "TestProbe"} {
		args := []string{"test", "-count=1", "-run", selector, "probe_test.go"}
		for _, jsonReport := range []bool{false, true} {
			if jsonReport {
				args = append([]string{"test", "-json"}, args[1:]...)
			}
			cmd := exec.CommandContext(t.Context(), "go", args...)
			cmd.Dir = dir
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("go test: %v\n%s", err, out)
			}
			task := observedTask(taskExited, 0, "", false)
			task.goReport = &emptyGoReport{}
			const diagnostic = "go: downloading example.com/probe v1.0.0"
			drainProcessOutput(task, bytes.NewReader(out), strings.NewReader(diagnostic+"\n"))
			<-task.drained
			task.cmd = "go " + strings.Join(args, " ")
			result := task.completeObservation(logsReturnBytes)
			if !strings.Contains(result.output, diagnostic) {
				t.Fatal("missing stderr in process output")
			}
			wantEmpty := selector == "TestMissing"
			if result.observation.NoTestsRun != wantEmpty || strings.Contains(formatProcessResult(result), "verification_hint") != wantEmpty {
				t.Fatalf("selector=%s, observation=%+v, output=%s", selector, result.observation, out)
			}
		}
	}
}

func TestEmptyGoTestReportRequiresWholePackageReport(t *testing.T) {
	const empty = "ok  \texample/a\t(cached) [no tests to run]\n"
	const noFiles = "?   \texample/b\t[no test files]\n"
	const uncovered = "\texample/c\t\tcoverage: 0.0% of statements\n"
	for _, tc := range []struct {
		command, output string
		want            bool
	}{
		{"go test ./...", empty + noFiles, true},
		{"go test -cover ./...", uncovered + noFiles, true},
		{"go test -cover ./...", uncovered + "ok  example/a  0.01s  coverage: 0.0% of statements", false},
		{"go test -cover ./...", "ok  example/a  0.01s  coverage: 0.0% of statements [no tests to run]", true},
		{"go test -cover ./...", "ok  example/a  0.01s  coverage: 0.0% of statements", false},
		{"go test -run Missing", "testing: warning: no tests to run\nPASS\nok  \texample/a\t0.002s", true},
		{"go test -cover -v ./...", "testing: warning: no tests to run\nPASS\ncoverage: 0.0% of statements\nok  example/a  0.01s  coverage: 0.0% of statements [no tests to run]", true},
		{"go test -v ./...", "testing: warning: no tests to run\nPASS\n" + empty + "ok  \texample/b\t0.002s", false},
		{"go test ./...", noFiles + "ok  \texample/a\t0.003s", false},
		{"go test ./... && echo done", empty, false},
		{"go test ./... | tail -1", empty, false},
		{"go test ./...", "custom output\n" + empty, false},
	} {
		if got := emptyGoTestReport(tc.command, []byte(tc.output)); got != tc.want {
			t.Errorf("%q %q: got %v", tc.command, tc.output, got)
		}
	}
}

func TestEmptyGoJSONReportBeyondLogRetention(t *testing.T) {
	for _, hasTest := range []bool{true, false} {
		task := observedTask(taskExited, 0, "", false)
		task.goReport = &emptyGoReport{}
		var stdout strings.Builder
		logLine := func(s string) { stdout.WriteString(s + "\n") }
		logLine(`{"Action":"start","Package":"p"}`)
		if hasTest {
			logLine(`{"Action":"pass","Package":"p","Test":"TestProbe"}`)
		}
		for i := 0; i < 320; i++ {
			logLine(`{"Action":"output","Package":"p","Output":"` + strings.Repeat("x", 900) + `"}`)
		}
		logLine(`{"Action":"output","Package":"p","Output":"testing: warning: no tests to run"}`)
		logLine(`{"Action":"pass","Package":"p"}`)
		drainProcessOutput(task, strings.NewReader(stdout.String()), nil)
		<-task.drained
		if task.logStart == 0 || task.completeObservation(logsReturnBytes).observation.NoTestsRun == hasTest {
			t.Fatalf("hasTest=%t ring start=%d", hasTest, task.logStart)
		}
	}
}

func TestEmptyJSONPackagesRequireCompleteReports(t *testing.T) {
	const start = `{"Action":"start","Package":"a"}` + "\n"
	const empty = `{"Action":"output","Package":"a","Output":"?   a [no test files]"}` + "\n"
	const end = `{"Action":"skip","Package":"a"}` + "\n"
	const other = `{"Action":"start","Package":"b"}` + "\n"
	const covered = `{"Action":"output","Package":"a","Output":"\ta\t\tcoverage: 0.0% of statements\n"}` + "\n"
	const pass = `{"Action":"pass","Package":"a"}` + "\n"
	const diagnostic = `{"ImportPath":"a [a.test]","Action":"build-output","Output":"compiler warning\n"}` + "\n"
	const tested = `{"Action":"run","Package":"b","Test":"TestProbe"}
{"Action":"pass","Package":"b","Test":"TestProbe"}
{"Action":"pass","Package":"b"}
`
	for _, tc := range []struct {
		output string
		want   bool
	}{
		{start + empty + end, true},
		{diagnostic + start + empty + diagnostic + end, true},
		{diagnostic, false},
		{diagnostic + start + empty, false},
		{diagnostic + start + empty + end + other + tested, false},
		{diagnostic + `{"ImportPath":"a","Action":"build-fail"}` + "\n" + start + empty + end, false},
		{start + covered + pass, true},
		{start + covered + pass + other + tested, false},
		{start + other + empty + end + strings.NewReplacer(`"a"`, `"b"`, " a ", " b ").Replace(empty+end), true},
		{start + other + empty + end, false},
		{start + empty, false},
		{start + empty + end + "malformed", false},
		{start + empty + `{"Action":"pass","Package":"a","Test":"TestProbe"}` + "\n" + end, false},
	} {
		if got := emptyGoTestReport("go test -json ./...", []byte(tc.output)); got != tc.want {
			t.Errorf("got %v for %s", got, tc.output)
		}
	}
}

func TestNamedTestReportAcrossReads(t *testing.T) {
	target := "TestRetry/network"
	for _, tc := range []struct{ stream, want string }{
		{`{"Action":"pass","Package":"a","Test":"TestRetry"}`, "missing"},
		{`{"Action":"output","Package":"a","Test":"TestRetry/network","Output":"PASS"}`, "missing"},
		{`{"Action":"pass","Package":"a","Test":"TestRetry/network"}`, "pass"},
		{`{"Action":"pass","Package":"a","Test":"TestRetry/network"}
{"Action":"skip","Package":"b","Test":"TestRetry/network"}`, "skip"},
		{`{"Action":"pass","Package":"a","Test":"TestRetry/network"}
broken`, "unverified"},
	} {
		report := namedGoTest{name: target}
		for _, b := range []byte(tc.stream + "\n") {
			report.Write([]byte{b})
		}
		if got := report.outcome(); got != tc.want {
			t.Errorf("%q: got %s", tc.stream, got)
		}
	}
}

func TestGoReportDrainsAfterExit(t *testing.T) {
	task := observedTask(taskExited, 0, "", false)
	task.goReport = &emptyGoReport{}
	task.goReport.Write([]byte("?    demo [no test files]\n"))
	early := task.completeObservation(logsReturnBytes)
	if !early.info.managed || early.observation.TestReportState != "pending" || early.observation.NoTestsRun {
		t.Fatalf("early: %+v", early.observation)
	}
	close(task.drained)
	final := task.completeObservation(logsReturnBytes).observation
	if final.TestReportState != "" || !final.NoTestsRun {
		t.Fatalf("after drain: %+v", final)
	}
	task.readFailed = true
	if got := task.completeObservation(logsReturnBytes).observation; got.TestReportState != "unreadable" || got.NoTestsRun {
		t.Fatalf("read failure: %+v", got)
	}
}
