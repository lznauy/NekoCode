package shell

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Match package-list summaries. Unknown or mixed reports carry no verdict.
const goCoverage = `coverage: (?:[0-9.]+% of statements|\[no statements\])`
const goPackageOK = `ok[[:blank:]]+\S+[[:blank:]]+(?:[0-9.]+s|\(cached\))(?:[[:blank:]]+` + goCoverage + `)?`

// With -cover, Go can report a package without test files as just its name
// and a coverage column, with no "ok" prefix or elapsed time.
const noTestFileCoverage = `\S+\t\tcoverage: (?:0\.0% of statements|\[no statements\])`

var coverageLine = regexp.MustCompile("^" + goCoverage + "$")
var verboseEmptyGoPackage = regexp.MustCompile("^" + goPackageOK + `(?:[[:blank:]]+\[no tests to run\])?$`)
var emptyGoPackage = regexp.MustCompile(`^(` + noTestFileCoverage + `|` + goPackageOK + `[[:blank:]]+\[no tests to run\]|\?[[:space:]]+\S+[[:space:]]+\[no test files\])$`)

func directGoTest(command string) bool {
	if !strings.HasPrefix(strings.TrimSpace(command), "go") {
		return false
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) != 1 {
		return false
	}
	stmt := file.Stmts[0]
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	return ok && !stmt.Negated && !stmt.Background && len(stmt.Redirs) == 0 && len(call.Args) >= 2 &&
		call.Args[0].Lit() == "go" && call.Args[1].Lit() == "test"
}

// Keep only open packages and the partial line. The console's retention
// limit must not affect the acceptance verdict for a complete report.
type emptyGoReport struct {
	line          []byte
	open          map[string]bool
	seen, invalid bool
	textStage     int // after an empty-test warning: expect PASS, optional coverage, then the package summary
}

func (r *emptyGoReport) Write(chunk []byte) (int, error) {
	n := len(chunk)
	for len(chunk) > 0 && !r.invalid {
		part, rest, complete := bytes.Cut(chunk, []byte("\n"))
		if len(r.line)+len(part) > 1<<20 {
			r.invalid = true
			r.line = nil
			break
		}
		r.line = append(r.line, part...)
		if complete {
			r.consume(bytes.TrimSpace(r.line))
			r.line = r.line[:0]
		}
		chunk = rest
	}
	return n, nil
}

func (r *emptyGoReport) consume(line []byte) {
	if !r.seen && len(line) == 0 {
		return
	}
	if !r.seen && r.open == nil && bytes.HasPrefix(line, []byte("{")) {
		r.open = make(map[string]bool)
	}
	if r.open != nil {
		var e struct{ Action, ImportPath, Package, Test, Output string }
		if json.Unmarshal(line, &e) != nil {
			r.invalid = true
			return
		}
		// go test -json can interleave compiler diagnostics with test events.
		// A build diagnostic alone says nothing about the tests selected.
		if e.Action == "build-output" && e.ImportPath != "" && e.Package == "" && e.Test == "" {
			return
		}
		r.seen = true
		if e.Test != "" || e.Package == "" {
			r.invalid = true
			return
		}
		empty, started := r.open[e.Package]
		switch e.Action {
		case "start":
			r.invalid = started
			r.open[e.Package] = false
		case "output":
			r.invalid = !started
			output := strings.TrimSpace(e.Output)
			r.open[e.Package] = empty || output == "testing: warning: no tests to run" || emptyGoPackage.MatchString(output)
		case "pass", "skip":
			r.invalid = !started || !empty
			delete(r.open, e.Package)
		default:
			r.invalid = true
		}
		return
	}
	r.seen = true
	switch r.textStage {
	case 0:
		if string(line) == "testing: warning: no tests to run" {
			r.textStage = 1
		} else {
			r.invalid = !emptyGoPackage.Match(line)
		}
	case 1:
		r.invalid = string(line) != "PASS"
		r.textStage = 2
	case 2, 3:
		if r.textStage == 2 && coverageLine.Match(line) {
			r.textStage = 3
			return
		}
		r.invalid = !verboseEmptyGoPackage.Match(line)
		r.textStage = 0
	}
}

func (r *emptyGoReport) finish() bool {
	if len(r.line) > 0 && !r.invalid {
		r.consume(bytes.TrimSpace(r.line))
		r.line = nil
	}
	return r.seen && !r.invalid && len(r.open) == 0 && r.textStage == 0
}

// The stdout drainer owns the report until it closes drained. Parsing before
// preview truncation keeps a named test visible even in a large package run.
type namedGoTest struct {
	name                             string
	line                             []byte
	invalid, passed, skipped, failed bool
}

func (r *namedGoTest) Write(chunk []byte) (int, error) {
	n := len(chunk)
	for len(chunk) > 0 && !r.invalid {
		part, rest, complete := bytes.Cut(chunk, []byte("\n"))
		if len(r.line)+len(part) > 1<<20 {
			r.invalid = true
			break
		}
		r.line = append(r.line, part...)
		if complete {
			var event struct{ Action, Package, Test string }
			if json.Unmarshal(r.line, &event) != nil || event.Action == "" {
				r.invalid = true
			} else if event.Package != "" && event.Test == r.name {
				switch event.Action {
				case "pass":
					r.passed = true
				case "skip":
					r.skipped = true
				case "fail":
					r.failed = true
				}
			}
			r.line = r.line[:0]
		}
		chunk = rest
	}
	return n, nil
}

func (r *namedGoTest) outcome() string {
	switch {
	case r.invalid || len(r.line) != 0:
		return "unverified"
	case r.failed:
		return "fail"
	case r.skipped:
		return "skip"
	case r.passed:
		return "pass"
	default:
		return "missing"
	}
}
