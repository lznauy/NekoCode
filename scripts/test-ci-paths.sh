#!/bin/sh

set -eu

classify='./scripts/classify-ci-paths.sh'

assert_paths() {
  expected="$(printf 'go=%s\nfrontend=%s\nwebsite=%s\nrepository=%s\ndocs=%s' "$1" "$2" "$3" "$4" "$5")"
  shift 5
  actual="$(printf '%s\0' "$@" | bash "$classify" false)"
  test "$actual" = "$expected" || {
    printf 'unexpected classification for: %s\nexpected:\n%s\nactual:\n%s\n' "$*" "$expected" "$actual" >&2
    exit 1
  }
}

assert_paths false false false false true 'docs/CI CD.md'
assert_paths true false false false true bot/prompt/system_zh.md
assert_paths true false false false true bot/agent/subagent/prompts/subagent.md
assert_paths true false false false true bot/extension/skill/bundled/meta/SKILL.md
assert_paths true false false false true bot/extension/tool/builtin/filesystem/edit/edit_description.md
assert_paths true false false false true mode/plan.md
assert_paths true false false false false bot/modelcatalog/snapshot.json
assert_paths true false false false false bot/extension/tool/runtime/sandbox/impl/browser-check.mjs
assert_paths true false false false false evals/agent_eval/runner.py
assert_paths true false false false true evals/cases/review-clean/prompt.md
assert_paths true false false false false evals/go.mod
assert_paths true true false false false build/makefile
assert_paths true true false false false main_runtime.go
assert_paths true true false false false interaction/gui/web/dist/index.html
assert_paths false true false false false interaction/gui/web/src/main.tsx
assert_paths false false false false true interaction/gui/web/README.md
assert_paths false false true false false official/app/page.tsx
assert_paths true false false true false .github/workflows/go.yml
assert_paths false false false true true .github/workflows/docs.yml
assert_paths false false false true false scripts/install.sh

expected_all="$(printf 'go=true\nfrontend=true\nwebsite=true\nrepository=true\ndocs=true')"
test "$(bash "$classify" true)" = "$expected_all"

echo 'CI path classification tests passed'
