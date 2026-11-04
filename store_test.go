package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(root, key)
		if e := os.MkdirAll(dir, 0o700); e != nil {
			t.Fatal(e)
		}
		t.Setenv(key, dir)
	}
	for _, key := range []string{"O_CMDS_AUTHOR", "TUIOS_AGENT_NAME", "AGENT_NAME", "TUIOS_HARNESS", "AGENT_HARNESS", "TUIOS_PANE_ID", "TUIOS_SESSION", "CODEX_THREAD_ID", "CODEX_CI", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "PI_SESSION_ID", "OCHAZUKE_ACTOR", "OCHAZUKE_HARNESS", "OCHAZUKE_MODEL", "AGENT_MODEL"} {
		t.Setenv(key, "")
	}
	return &Store{Dir: filepath.Join(root, "commands"), Now: func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }}
}

func addCommand(t *testing.T, s *Store, name, script string) Command {
	t.Helper()
	c, e := s.Add(Add{Name: name, Script: script})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestStoreAddListShowOrderAndDuplicate(t *testing.T) {
	s := fixture(t)
	at := s.now()
	s.Now = func() time.Time { return at }
	first := addCommand(t, s, "hello", "printf first\n")
	at = at.Add(time.Second)
	second := addCommand(t, s, "hello", "printf second\n")
	if second.Name != "hello-2" {
		t.Fatalf("duplicate name=%q", second.Name)
	}
	rows, e := s.List()
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 || rows[0].ID != first.ID || rows[1].ID != second.ID {
		t.Fatalf("order=%+v", rows)
	}
	c, e := s.Resolve("2", false)
	if e != nil || c.ID != second.ID {
		t.Fatalf("number=%+v %v", c, e)
	}
	text, e := s.Script(c)
	if e != nil || text != "printf second\n" {
		t.Fatalf("script=%q %v", text, e)
	}
	if c.Author.Name != "owner" {
		t.Fatalf("author=%+v", c.Author)
	}
	if !strings.HasPrefix(filepath.Base(c.Path), "20261030T120001.") {
		t.Fatalf("path=%s", c.Path)
	}
	info, e := os.Stat(c.Path)
	if e != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v %v", info, e)
	}
}

func TestStoreRenameDeleteUndoAndEdit(t *testing.T) {
	s := fixture(t)
	one := addCommand(t, s, "one", "original\n")
	addCommand(t, s, "two", "other\n")
	c, e := s.Change(one.ID, func(c *Command, rows []Command) { c.Name = uniqueName("two", rows, c.ID) })
	if e != nil || c.Name != "two-2" {
		t.Fatalf("rename=%+v %v", c, e)
	}
	_, e = s.Change(one.ID, func(c *Command, _ []Command) { c.Deleted = true })
	if e != nil {
		t.Fatal(e)
	}
	rows, _ := s.List()
	if len(rows) != 1 {
		t.Fatalf("after delete=%+v", rows)
	}
	if b, e := os.ReadFile(one.Path); e != nil || string(b) != "original\n" {
		t.Fatalf("deleted bytes lost: %q %v", b, e)
	}
	c, e = s.Undo()
	if e != nil || c.Name != "two-2" || c.Deleted {
		t.Fatalf("undo delete=%+v %v", c, e)
	}
	c, e = s.Undo()
	if e != nil || c.Name != "one" {
		t.Fatalf("undo rename=%+v %v", c, e)
	}
	_, e = s.Change(one.ID, func(*Command, []Command) {})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(one.Path, []byte("edited\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	c, e = s.Undo()
	if e != nil {
		t.Fatal(e)
	}
	text, e := s.Script(c)
	if e != nil || text != "original\n" {
		t.Fatalf("undo edit=%q %v", text, e)
	}
}

func TestStoreRecordFirstAndUnassociated(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "", "")
	if c.Name != "command" {
		t.Fatalf("name=%q", c.Name)
	}
	// Interrupted metadata writes still leave a discoverable script.
	path := filepath.Join(s.Dir, "orphan.sh")
	if e := os.WriteFile(path, []byte("echo orphan"), 0o600); e != nil {
		t.Fatal(e)
	}
	rows, e := s.List()
	if e != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v %v", rows, e)
	}
	got, e := s.Resolve("orphan", false)
	if e != nil || got.Author.Name != "unassociated" {
		t.Fatalf("orphan=%+v %v", got, e)
	}
	missing := filepath.Join(s.Dir, "does-not-exist")
	_, e = s.Add(Add{Name: "context", Script: "", Cwd: missing})
	if e != nil {
		t.Fatalf("missing cwd refused: %v", e)
	}
}

func TestStoreConcurrentAdds(t *testing.T) {
	s := fixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Add(Add{Name: "same", Script: "echo hi"}); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	rows, e := s.List()
	if e != nil || len(rows) != 12 {
		t.Fatalf("count=%d %v", len(rows), e)
	}
	names := map[string]bool{}
	for _, c := range rows {
		names[c.Name] = true
	}
	for i := 1; i <= 12; i++ {
		name := "same"
		if i > 1 {
			name = fmt.Sprintf("same-%d", i)
		}
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestAuthorStamping(t *testing.T) {
	fixture(t)
	cases := []struct {
		name     string
		env      map[string]string
		fallback string
		want     Author
	}{
		{"owner", nil, "owner", Author{Name: "owner"}},
		{"agent", map[string]string{"AGENT_NAME": "sol", "AGENT_HARNESS": "codex", "TUIOS_PANE_ID": "p7", "TUIOS_SESSION": "s1"}, "owner", Author{Name: "sol", Harness: "codex", Pane: "p7", Session: "s1"}},
		{"codex", map[string]string{"CODEX_THREAD_ID": "thread"}, "owner", Author{Name: "codex", Harness: "codex"}},
		{"mcp", nil, "broker", Author{Name: "broker"}},
		{"override", map[string]string{"O_CMDS_AUTHOR": "Ada", "AGENT_NAME": "agent"}, "owner", Author{Name: "Ada"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			get := func(k string) string { return tt.env[k] }
			if got := stampAuthor(get, tt.fallback); got != tt.want {
				t.Fatalf("got=%+v want=%+v", got, tt.want)
			}
		})
	}
}
