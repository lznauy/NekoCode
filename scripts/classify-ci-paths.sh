#!/usr/bin/env bash

set -euo pipefail

go=false
frontend=false
website=false
repository=false
docs=false

if [[ "${1:-false}" == "true" ]]; then
  go=true
  frontend=true
  website=true
  repository=true
  docs=true
else
  while IFS= read -r -d '' path; do
    [[ "$path" == *.md ]] && docs=true

    case "$path" in
      *.go | go.mod | go.sum | build/makefile | build/shell.nix | mode/*.md | bot/modelcatalog/snapshot.json | bot/extension/tool/runtime/sandbox/impl/browser-check.mjs | bot/prompt/*.md | bot/agent/subagent/prompts/*.md | bot/extension/skill/bundled/*.md | bot/extension/tool/builtin/filesystem/edit/*.md | evals/go.mod | evals/go.sum | evals/agent_eval/* | evals/tests/* | evals/cases/* | evals/suites/* | evals/browser-package.json | interaction/gui/web/dist/* | .github/workflows/go.yml)
        go=true
        ;;
    esac

    case "$path" in
      interaction/gui/web/*.md) ;;
      interaction/gui/web/* | main*.go | interaction/gui/app/* | build/makefile | wails.json | .github/workflows/frontend.yml)
        frontend=true
        ;;
    esac

    case "$path" in
      official/*.md) ;;
      official/* | .github/workflows/website.yml)
        website=true
        ;;
    esac

    case "$path" in
      .github/* | scripts/*)
        repository=true
        ;;
    esac

    [[ "$path" == ".github/workflows/docs.yml" ]] && docs=true
  done
fi

printf 'go=%s\n' "$go"
printf 'frontend=%s\n' "$frontend"
printf 'website=%s\n' "$website"
printf 'repository=%s\n' "$repository"
printf 'docs=%s\n' "$docs"
