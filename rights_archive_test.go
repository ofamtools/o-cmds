package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func TestAgentArchiveRestoreAndReset(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "retained", "printf 'retained bytes\\n'\n")
	other := addCommand(t, s, "visible", "true\n")
	if e := s.CountRun(c.ID, s.now()); e != nil {
		t.Fatal(e)
	}
	seedRun(t, s, c, "retained-run", s.now())
	actor := ochazuke.Actor{Kind: "agent", Name: "writer", Harness: "test", Pane: "pane", Session: "session", Model: "model"}
	raw, e := json.Marshal(actor)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("OCHAZUKE_ACTOR", string(raw))
	call := func(args ...string) string {
		t.Helper()
		out, e := invoke(t, s, "", args...)
		if e != nil {
			t.Fatalf("%v: %v", args, e)
		}
		return out
	}
	for _, args := range [][]string{{"cmd", "delete", c.ID}, {"run", "delete", "retained-run"}, {"cmd", "runs-clear", c.ID}} {
		if _, e := invoke(t, s, "", args...); e == nil {
			t.Fatalf("agent deleted: %v", args)
		}
	}
	for range 2 {
		call("cmd", "archive", c.ID)
	}
	rows, e := s.List()
	if e != nil || len(rows) != 1 || rows[0].ID != other.ID {
		t.Fatal(rows, e)
	}
	if numbered, e := s.Resolve("1", false); e != nil || numbered.ID != other.ID {
		t.Fatal(numbered, e)
	}
	if text, e := s.Script(c); e != nil || text != "printf 'retained bytes\\n'\n" {
		t.Fatal(text, e)
	}
	call("setting", "set", "list.hide-archived", "false", "--scope", "file")
	var listed []ocli.Row
	if e := json.Unmarshal([]byte(call("cmd", "list", "--json", "{}")), &listed); e != nil {
		t.Fatal(e)
	}
	if len(listed) != 2 || listed[0].ID != "cmd/"+c.ID || listed[0].Cells[0] != "-" || listed[1].Cells[0] != "1" {
		t.Fatal(listed)
	}
	for range 2 {
		call("cmd", "restore", c.ID)
	}
	rows, e = s.List()
	if e != nil || len(rows) != 2 || rows[0].ID != c.ID || rows[0].Deleted {
		t.Fatal(rows, e)
	}
	call("cmd", "reset", c.ID)
	current, e := s.Resolve(c.ID, false)
	if e != nil {
		t.Fatal(e)
	}
	runs, e := s.Runs(c.ID)
	if e != nil || len(runs) != 1 || runs[0].ID != "retained-run" {
		t.Fatal(runs, e)
	}
	if state := runStateOf(current, runs); state.Count != 0 || state.Status != statusNew {
		t.Fatal(state)
	}
	files, e := filepath.Glob(filepath.Join(s.Dir, ".history", "*.json"))
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 5 {
		t.Fatal(files)
	}
	for _, file := range files {
		b, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		var snapshot Snapshot
		if e = json.Unmarshal(b, &snapshot); e != nil || snapshot.By != actor {
			t.Fatal(snapshot, e)
		}
	}
	call("cmd", "undo")
	current, e = s.Resolve(c.ID, false)
	if e != nil || runStateOf(current, runs).Count != 1 {
		t.Fatal(current, e)
	}
}

func TestCommandRightsCatalog(t *testing.T) {
	s := fixture(t)
	out, e := invoke(t, s, "", "catalog")
	if e != nil {
		t.Fatal(e)
	}
	var catalog ocli.CatalogDoc
	if e = json.Unmarshal([]byte(out), &catalog); e != nil {
		t.Fatal(e)
	}
	wants := map[string]struct {
		right       string
		destructive bool
	}{
		"cmd.delete":     {ochazuke.AgentNever, false},
		"cmd.archive":    {ochazuke.AgentAllow, false},
		"cmd.restore":    {ochazuke.AgentAllow, false},
		"cmd.reset":      {ochazuke.AgentAllow, false},
		"cmd.runs-clear": {ochazuke.AgentNever, true},
		"run.delete":     {ochazuke.AgentNever, true},
	}
	for _, noun := range catalog.Nouns {
		for _, verb := range noun.Verbs {
			key := noun.Name + "." + verb.Name
			want, ok := wants[key]
			if !ok {
				continue
			}
			if verb.Agent != want.right || verb.Destructive != want.destructive {
				t.Fatalf("%s: %+v", key, verb)
			}
			delete(wants, key)
		}
	}
	if len(wants) != 0 {
		t.Fatalf("missing rows: %v", wants)
	}
}

func TestRestoreKeepsNamesDistinct(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "same", "original\n")
	if _, e := invoke(t, s, "", "cmd", "archive", c.ID); e != nil {
		t.Fatal(e)
	}
	live := addCommand(t, s, "same", "new\n")
	if got, e := s.Resolve("same", false); e != nil || got.ID != live.ID {
		t.Fatal(got, e)
	}
	if _, e := invoke(t, s, "", "cmd", "restore", c.ID); e != nil {
		t.Fatal(e)
	}
	got, e := s.Resolve(c.ID, false)
	if e != nil || got.Name != "same-2" {
		t.Fatal(got, e)
	}
	if text, e := s.Script(got); e != nil || strings.TrimSpace(text) != "original" {
		t.Fatal(text, e)
	}
}
