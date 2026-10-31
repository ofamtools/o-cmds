package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandMatchingPrecedenceAndDescendants(t *testing.T) {
	rows := []Command{
		{ID: "id1", Name: "play-o-json-tutorials"},
		{ID: "id2", Name: "play"},
		{ID: "id3", Name: "play-o-json"},
		{ID: "id4", Name: "2"},
		{ID: "id5", Name: "4-extra"},
	}
	for _, tt := range []struct{ query, want string }{
		{"play", "id2"},
		{"1", "id1"},
		{"2", "id4"},
		{"4", "id4"},
		{"play-o-json-tutorials", "id1"},
		{"cmd/id1", "id1"},
		{"pla", "id2"},
		{"play-o-j", "id3"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			got, e := resolve(rows, tt.query)
			if e != nil || got.ID != tt.want {
				t.Fatalf("got=%+v error=%v want=%s", got, e, tt.want)
			}
		})
	}
	if _, e := resolve(rows, "99"); e == nil {
		t.Fatal("out-of-range number accepted")
	}
}

func TestCommandMatchingAmbiguousNumberedInDisplayOrder(t *testing.T) {
	rows := []Command{{ID: "1", Name: "zebra"}, {ID: "2", Name: "play-two"}, {ID: "3", Name: "play-one"}, {ID: "4", Name: "other"}}
	_, e := resolve(rows, "play")
	want := "command \"play\" is ambiguous; matching commands:\n  2  play-two\n  3  play-one"
	if e == nil || e.Error() != want {
		t.Fatalf("ambiguity=%v want=%s", e, want)
	}
	if _, e = resolve(rows, ""); e == nil {
		t.Fatal("empty target selected a command")
	}
}

func TestCommandMatchingEveryTargetVerb(t *testing.T) {
	for _, verb := range []string{"show", "run", "copy", "edit", "rename", "delete", "pin", "unpin", "star", "path"} {
		t.Run(verb, func(t *testing.T) {
			s := fixture(t)
			command := addCommand(t, s, "play-o-json-tutorials", "printf tutorial\n")
			var out bytes.Buffer
			a, e := newApp(s, strings.NewReader(""), &out, &out)
			if e != nil {
				t.Fatal(e)
			}
			called := false
			a.run = func(cmd *exec.Cmd) error {
				called = true
				if cmd.Args[len(cmd.Args)-1] != command.Path {
					t.Fatalf("process args=%q", cmd.Args)
				}
				return nil
			}
			args := []string{"cmd", verb, "play"}
			if verb == "rename" {
				args = append(args, "renamed")
			}
			if e = dispatch(a, args); e != nil {
				t.Fatal(e)
			}
			if (verb == "run" || verb == "edit") && !called {
				t.Fatal("process runner was not called")
			}
		})
	}
}

func TestCommandMatchingAmbiguityNeverActs(t *testing.T) {
	for _, verb := range []string{"show", "run", "copy", "edit", "rename", "delete", "pin", "unpin", "star", "path"} {
		t.Run(verb, func(t *testing.T) {
			s := fixture(t)
			addCommand(t, s, "play-one", "one")
			addCommand(t, s, "play-two", "two")
			var out bytes.Buffer
			a, e := newApp(s, strings.NewReader(""), &out, &out)
			if e != nil {
				t.Fatal(e)
			}
			a.run = func(*exec.Cmd) error { t.Fatal("ambiguous input executed a process"); return nil }
			args := []string{"cmd", verb, "play"}
			if verb == "rename" {
				args = append(args, "renamed")
			}
			if e = dispatch(a, args); e == nil || !strings.Contains(e.Error(), "1  play-one\n  2  play-two") {
				t.Fatalf("ambiguity=%v", e)
			}
			rows, e := s.List()
			if e != nil || len(rows) != 2 || rows[0].Name != "play-one" || rows[1].Name != "play-two" || rows[0].Pinned || rows[1].Pinned {
				t.Fatalf("ambiguous call mutated records: %+v %v", rows, e)
			}
		})
	}
}

func TestCommandMatchingMCPShow(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "play-o-json-tutorials", "tutorial")
	var out bytes.Buffer
	a, e := newApp(s, strings.NewReader(""), &out, &out)
	if e != nil {
		t.Fatal(e)
	}
	for _, target := range []string{"play", "1", "play-o-json-tutorials"} {
		raw, _ := json.Marshal(map[string]string{"target": target})
		result := a.callTool("cmd_show", raw, "test")
		b, _ := json.Marshal(result)
		if strings.Contains(string(b), `"isError":true`) || !strings.Contains(string(b), "tutorial") {
			t.Fatalf("MCP %q: %s", target, b)
		}
	}
	addCommand(t, s, "play-other", "other")
	result := a.callTool("cmd_show", json.RawMessage(`{"target":"play"}`), "test")
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), `"isError":true`) || !strings.Contains(string(b), "1  play-o-json-tutorials") || !strings.Contains(string(b), "2  play-other") {
		t.Fatalf("MCP ambiguity: %s", b)
	}
}

// These are the requested shared-oden capabilities. Remove the explicit skips
// when the coordinator supplies an oden release implementing these stages.
// No app-side fallback matcher should make these examples pass.
func TestCommandNameAbbreviations(t *testing.T) {
	t.Skip("blocked: oden v0.1.1 only implements exact/prefix matching; lane is restricted to o-cmds and cannot add a second matcher")
	for _, query := range []string{"p-o-j-t", "pojt", "tutorial"} {
		t.Run(query, func(t *testing.T) {
			rows := []Command{{ID: "wanted", Name: "play-o-json-tutorials"}, {ID: "other", Name: "build-tools"}}
			got, e := resolve(rows, query)
			if e != nil || got.ID != "wanted" {
				t.Fatalf("query=%q got=%+v error=%v", query, got, e)
			}
		})
	}
}

func TestCommandNameAbbreviationsAmbiguous(t *testing.T) {
	t.Skip("blocked: oden v0.1.1 lacks dash-segment and fragment stages; coordinator must supply the shared matcher")
	rows := []Command{{ID: "first", Name: "play-o-json-tutorials"}, {ID: "second", Name: "print-o-json-tips"}}
	for _, query := range []string{"p-o-j-t", "pojt", "json"} {
		t.Run(query, func(t *testing.T) {
			_, e := resolve(rows, query)
			if e == nil || !strings.Contains(e.Error(), "ambiguous") || !strings.Contains(e.Error(), "1  play-o-json-tutorials") || !strings.Contains(e.Error(), "2  print-o-json-tips") {
				t.Fatalf("ambiguity=%v", e)
			}
		})
	}
}
