#!/usr/bin/env bash
# PostToolUse hook: format the Go/templ file Claude just wrote. Never fails the tool call.
set -u
file=$(jq -r '.tool_input.file_path // empty' 2>/dev/null) || exit 0
[ -n "$file" ] && [ -f "$file" ] || exit 0
case "$file" in
  *_templ.go) ;;                                   # generated: leave alone
  *.go)    command -v gofmt >/dev/null && gofmt -w "$file" ;;
  *.templ) if [ -f "${CLAUDE_PROJECT_DIR:-.}/go.mod" ]; then (cd "${CLAUDE_PROJECT_DIR:-.}" && go tool templ fmt "$file" >/dev/null 2>&1); elif command -v templ >/dev/null; then templ fmt "$file" >/dev/null 2>&1; fi ;;
esac
exit 0
