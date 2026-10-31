package main

import (
	"encoding/json"
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

// The published catalog declares sections (rendered by every host) and keeps
// the record fields for hosts with detail.sections off; the rows carry the
// fields those sections read.
func TestCatalogDetailSectionsAndRowFields(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "multi", "echo one\n\necho two\n")
	out, e := invoke(t, s, "", "catalog")
	if e != nil {
		t.Fatal(e)
	}
	var doc struct {
		Pages []struct {
			ID     string
			Detail struct {
				Kind     string
				Fields   []string
				Sections []struct{ Kind, Field string }
			}
		}
	}
	if e = json.Unmarshal([]byte(out), &doc); e != nil {
		t.Fatal(e)
	}
	if len(doc.Pages) != 1 || doc.Pages[0].Detail.Kind != "" || len(doc.Pages[0].Detail.Sections) != 2 || doc.Pages[0].Detail.Sections[1].Field != "detail_md" || len(doc.Pages[0].Detail.Fields) == 0 {
		t.Fatalf("catalog detail=%+v", doc.Pages)
	}
	a, e := newApp(s, strings.NewReader(""), &strings.Builder{}, &strings.Builder{})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := a.rows("")
	if e != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, e)
	}
	data := rows[0].Data.(map[string]any)
	if data["detail_md"] != "```sh\necho one\n\necho two\n```\n" || data["author_label"] == "" {
		t.Fatalf("row data=%v", data)
	}
}
