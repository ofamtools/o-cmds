package main

import (
	"strings"
	"testing"
)

func TestScriptLanguage(t *testing.T) {
	for _, c := range []struct{ script, path, want string }{
		{"echo hi\n", "/x/abc.sh", "sh"},
		{"echo hi\n", "", "sh"},
		{"#!/bin/bash\necho\n", "/x/a.sh", "bash"},
		{"#!/usr/bin/env -S python3 -u\nprint()\n", "", "python"},
		{"#!/usr/bin/env FOO=1 node\n", "", "javascript"},
		{"#!/usr/local/bin/zsh -f\n", "", "zsh"},
		{"#!/opt/bin/janet\n", "", "janet"},
		{"#!/opt/bin/we!rd\n", "/x/a.rb", "ruby"},
		{"print(1)\n", "/x/a.py", "python"},
	} {
		if got := scriptLanguage(c.script, c.path); got != c.want {
			t.Errorf("scriptLanguage(%q, %q) = %q, want %q", c.script, c.path, got, c.want)
		}
	}
}

func TestScriptMarkdownKeepsLayoutAndOutfencesBackticks(t *testing.T) {
	src := "if true; then\n\tprintf 'one'\n\n    printf 'two'\nfi\n"
	if got, want := scriptMarkdown(src, "a.sh"), "```sh\n"+strings.TrimSuffix(src, "\n")+"\n```\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	inner := "cat <<'EOF'\n````\nx\n```\nEOF"
	got := scriptMarkdown(inner, "")
	if !strings.HasPrefix(got, "`````sh\n"+inner+"\n`````\n") {
		t.Fatalf("fence not longer than inner backticks: %q", got)
	}
	if got := scriptMarkdown("", ""); got != "```sh\n\n```\n" {
		t.Fatalf("empty script: %q", got)
	}
}
