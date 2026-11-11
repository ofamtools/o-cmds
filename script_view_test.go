package main

import (
	"bytes"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestScriptFormattingKeepsWhitespace(t *testing.T) {
	for _, tt := range []struct{ source, want string }{
		{"", "1 │ "},
		{"one\n\n", "1 │ one\n2 │ "},
		{"if true; then\n    printf 'x  y'\n\nfi\n", "1 │ if true; then\n2 │     printf 'x  y'\n3 │ \n4 │ fi"},
		{"\tprintf 'tab'\n", "1 │ \tprintf 'tab'"},
	} {
		if got := numberedScript(tt.source); got != tt.want {
			t.Fatalf("numbered=%q want=%q", got, tt.want)
		}
	}
	line := "     printf     'long    spacing'     "
	if got := strings.ReplaceAll(scriptAtWidth(line, 8), "\n", ""); got != line {
		t.Fatalf("wrapping changed spaces: %q", got)
	}
	if got := scriptAtWidth("\tindented", 80); got != "    indented" {
		t.Fatalf("tab rendering=%q", got)
	}
}

func TestScriptViewDoesNotChangeSourceResults(t *testing.T) {
	s := fixture(t)
	t.Setenv("O_CMDS_CLIPBOARD", "cat >/dev/null")
	source := "\tif true; then\n    printf 'two  spaces'\n\nfi\n\n"
	addCommand(t, s, "source", source)
	for _, args := range [][]string{{"cmd", "show-script", "source"}, {"cmd", "yank", "source"}, {"pick", "source"}} {
		out, e := invoke(t, s, "", args...)
		if e != nil || out != source {
			t.Fatalf("%q output=%q error=%v", args, out, e)
		}
	}
}

func TestScriptOrderBindingUsesExistingSettingCycle(t *testing.T) {
	s := fixture(t)
	var out bytes.Buffer
	a, e := newApp(s, strings.NewReader(""), &out, &out)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range a.r.Nouns["cmd"].Bindings["normal"] {
		if row.Keys == "o" {
			if row.Request.Key() != "ochazuke.setting.cycle" || ochazuke.StringArg(row.Request.Args, "name", "") != "list.order" {
				t.Fatalf("order row=%+v", row)
			}
			return
		}
	}
	t.Fatal("missing order binding")
}
