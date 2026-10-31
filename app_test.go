package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/oden"
)

func invoke(t *testing.T, s *Store, input string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	a, e := newApp(s, strings.NewReader(input), &out, &errOut)
	if e != nil {
		t.Fatal(e)
	}
	e = dispatch(a, args)
	if e != nil {
		t.Logf("stderr: %s", errOut.String())
	}
	return out.String(), e
}

func TestCLIAddListShowRenameDeleteUndo(t *testing.T) {
	s := fixture(t)
	at := s.now()
	s.Now = func() time.Time { return at }
	out, e := invoke(t, s, "printf 'old\\n'\n", "cmd", "add", "--name", "older", "--desc", "first script")
	if e != nil {
		t.Fatal(e)
	}
	var first Command
	if e = json.Unmarshal([]byte(out), &first); e != nil {
		t.Fatal(e)
	}
	at = at.Add(time.Second)
	file := filepath.Join(t.TempDir(), "source.sh")
	if e = os.WriteFile(file, []byte("printf 'new\\n'\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	_, e = invoke(t, s, "", "cmd", "add", "--file", file, "--name", "newer")
	if e != nil {
		t.Fatal(e)
	}
	out, e = invoke(t, s, "", "cmd", "list")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Index(out, "older") < 0 || strings.Index(out, "newer") < strings.Index(out, "older") {
		t.Fatalf("order:\n%s", out)
	}
	for _, args := range [][]string{{"cmd", "show", "1"}, {"cmd", "show", "--target", "cmd/" + first.ID}} {
		out, e = invoke(t, s, "", args...)
		if e != nil || out != "printf 'old\\n'\n" {
			t.Fatalf("show=%q %v", out, e)
		}
	}
	out, e = invoke(t, s, "", "cmd", "show", "--target", "cmd/"+first.ID, "--json", "{}", "--yes")
	if e != nil || !json.Valid([]byte(out)) {
		t.Fatalf("host show=%q %v", out, e)
	}
	_, e = invoke(t, s, "", "cmd", "rename", "older", "renamed")
	if e != nil {
		t.Fatal(e)
	}
	_, e = invoke(t, s, "", "cmd", "delete", "renamed")
	if e != nil {
		t.Fatal(e)
	}
	_, e = invoke(t, s, "", "cmd", "undo")
	if e != nil {
		t.Fatal(e)
	}
	out, e = invoke(t, s, "", "cmd", "copy", "renamed")
	if e != nil || out != "printf 'old\\n'\n" {
		t.Fatalf("copy=%q %v", out, e)
	}
	out, e = invoke(t, s, "", "cmd", "path", "renamed")
	if e != nil || strings.TrimSpace(out) != first.Path {
		t.Fatalf("path=%q %v", out, e)
	}
}

func TestCLIRunExitArgsCwdAndStreams(t *testing.T) {
	s := fixture(t)
	cwd := t.TempDir()
	c, e := s.Add(Add{Name: "exit", Cwd: cwd, Script: "printf '%s\\n' \"$PWD\" \"$1\" \"$2\"\ncat\nprintf 'err\\n' >&2\nexit 7\n"})
	if e != nil {
		t.Fatal(e)
	}
	var out, errOut bytes.Buffer
	a, e := newApp(s, strings.NewReader("stdin\n"), &out, &errOut)
	if e != nil {
		t.Fatal(e)
	}
	e = dispatch(a, []string{"cmd", "run", c.Name, "--", "with spaces", "--data"})
	if oden.ExitCode(e) != 7 {
		t.Fatalf("exit=%v", e)
	}
	if out.String() != cwd+"\nwith spaces\n--data\nstdin\n" || errOut.String() != "err\n" {
		t.Fatalf("out=%q err=%q", out.String(), errOut.String())
	}
	// A destructive-looking script only reaches a fake injected runner.
	danger := addCommand(t, s, "danger", "rm -rf -- /does-not-exist\n")
	a.run = func(cmd *exec.Cmd) error {
		if cmd.Args[1] != danger.Path {
			t.Fatalf("args=%q", cmd.Args)
		}
		return nil
	}
	if e = dispatch(a, []string{"cmd", "run", danger.Name}); e != nil {
		t.Fatal(e)
	}
}

func TestCLISettingsAndPin(t *testing.T) {
	s := fixture(t)
	first := addCommand(t, s, "old", "echo old")
	s.Now = func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) }
	addCommand(t, s, "new", "echo new")
	_, e := invoke(t, s, "", "settings", "set", "list.order", "newest-first", "--yes")
	if e != nil {
		t.Fatal(e)
	}
	out, e := invoke(t, s, "", "cmd", "show", "1")
	if e != nil || out != "echo new" {
		t.Fatalf("order setting=%q %v", out, e)
	}
	_, e = invoke(t, s, "", "cmd", "pin", first.Name)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Resolve(first.Name, false)
	if e != nil || !c.Pinned {
		t.Fatalf("pin=%+v %v", c, e)
	}
	_, e = invoke(t, s, "", "cmd", "star", first.Name)
	if e != nil {
		t.Fatal(e)
	}
	c, _ = s.Resolve(first.Name, false)
	if c.Pinned {
		t.Fatal("star did not toggle")
	}
}

func TestCatalogAndStandalonePage(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "old", "echo old")
	s.Now = func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) }
	newest := addCommand(t, s, "new", "echo new")
	out, e := invoke(t, s, "", "catalog")
	if e != nil {
		t.Fatal(e)
	}
	var doc ocli.CatalogDoc
	if e = json.Unmarshal([]byte(out), &doc); e != nil {
		t.Fatal(e)
	}
	if doc.Name != "o-cmds" || len(doc.Pages) != 1 || doc.Pages[0].Lists[0].Source != "cmd.list" {
		t.Fatalf("catalog=%+v", doc.Pages)
	}
	keys := map[string]string{}
	for _, b := range doc.Bindings {
		if b.Noun == "cmd" {
			keys[b.Keys] = b.Request.Key()
		}
	}
	for key, verb := range map[string]string{"enter": "cmd.run", "v": "cmd.show", "y": "cmd.copy", "e": "cmd.edit", "d d": "cmd.delete"} {
		if keys[key] != verb {
			t.Errorf("binding %s=%s", key, keys[key])
		}
	}
	var buf bytes.Buffer
	a, e := newApp(s, strings.NewReader(""), &buf, &buf)
	if e != nil {
		t.Fatal(e)
	}
	p, e := mountPage(a)
	if e != nil {
		t.Fatal(e)
	}
	selected, ok := p.v.List(listID).Selected()
	if !ok || selected.Data != "cmd/"+newest.ID {
		t.Fatalf("selection=%+v", selected)
	}
	// Real model dispatch, with a foreground hook: Enter starts a shell
	// command without executing it inside a unit test.
	var fg *exec.Cmd
	p.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd { fg = msg.Cmd; return func() tea.Msg { return msg.Done(nil) } }
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		deliver(t, p, cmd())
	}
	if fg == nil || len(fg.Args) < 2 || fg.Args[1] != newest.Path {
		t.Fatalf("foreground=%v", fg)
	}
}

func deliver(t *testing.T, p *page, msg tea.Msg) {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, cmd := range batch {
			if cmd != nil {
				deliver(t, p, cmd())
			}
		}
		return
	}
	_, cmd := p.Update(msg)
	if cmd != nil {
		deliver(t, p, cmd())
	}
}

func TestGlobalDataPreservesRunArgs(t *testing.T) {
	s := fixture(t)
	args, dir, e := globalData([]string{"--data", s.Dir, "cmd", "run", "x", "--", "--data", "leave"})
	if e != nil || dir != s.Dir || strings.Join(args, " ") != "cmd run x -- --data leave" {
		t.Fatalf("args=%q dir=%q e=%v", args, dir, e)
	}
}

func TestViewSmokeOwnsFrameworkAlias(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "smoke-command", "echo hello")
	out, e := invoke(t, s, "", "view", "--smoke", "--width", "100", "--height", "20")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out, "smoke-command") {
		t.Fatalf("frame=%s", out)
	}
}

func TestPageDetailUsesDeclaredFields(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "detail-command", "printf hello\n")
	out, e := invoke(t, s, "", "view", "--smoke", "--width", "120", "--height", "26")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out, `"script":`) || !strings.Contains(out, "printf hello") {
		t.Fatalf("detail rendered a JSON blob instead of fields:\n%s", out)
	}
}

func TestPageFilterDeleteUndoRefreshAndOptOut(t *testing.T) {
	s := fixture(t)
	old := addCommand(t, s, "older", "echo old")
	addCommand(t, s, "newer", "echo new")
	var out bytes.Buffer
	a, e := newApp(s, strings.NewReader(""), &out, &out)
	if e != nil {
		t.Fatal(e)
	}
	p, e := mountPage(a)
	if e != nil {
		t.Fatal(e)
	}
	press := func(code rune) {
		_, cmd := p.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
		if cmd != nil {
			deliver(t, p, cmd())
		}
	}
	press('d')
	press('d')
	rows, e := s.List()
	if e != nil || len(rows) != 1 || rows[0].ID != old.ID {
		t.Fatalf("delete rows=%+v %v", rows, e)
	}
	press('u')
	rows, e = s.List()
	if e != nil || len(rows) != 2 {
		t.Fatalf("undo rows=%+v %v", rows, e)
	}
	press('/')
	press('n')
	press('e')
	press('w')
	list := p.v.List(listID)
	if list.Shown() != 1 {
		t.Fatalf("filter shown=%d", list.Shown())
	}
	// A separately added command reaches an already-mounted standalone page.
	addCommand(t, s, "newest", "echo newest")
	if e = a.r.Settings().Set("list.refresh", time.Duration(0)); e != nil {
		t.Fatal(e)
	}
	_, cmd := p.Update(reloadMsg{})
	if cmd != nil {
		t.Fatal("disabled refresh scheduled a timer")
	}
	if list.Shown() != 2 {
		t.Fatalf("refresh filtered rows=%d", list.Shown())
	}
}

func TestEditAndClipboardUseInjectedRunner(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "sample", "old bytes\n")
	var out bytes.Buffer
	a, e := newApp(s, strings.NewReader(""), &out, &out)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("O_CMDS_CLIPBOARD", "fake-clipboard")
	a.run = func(cmd *exec.Cmd) error {
		b := new(bytes.Buffer)
		if _, e := b.ReadFrom(cmd.Stdin); e != nil {
			t.Fatal(e)
		}
		if b.String() != "old bytes\n" {
			t.Fatalf("clipboard=%q", b.String())
		}
		return nil
	}
	if e = dispatch(a, []string{"cmd", "copy", c.Name, "--yank"}); e != nil {
		t.Fatal(e)
	}
	a.run = func(cmd *exec.Cmd) error {
		if got := cmd.Args[len(cmd.Args)-1]; got != c.Path {
			t.Fatalf("editor path=%q", got)
		}
		return os.WriteFile(c.Path, []byte("changed bytes\n"), 0o600)
	}
	if e = dispatch(a, []string{"cmd", "edit", c.Name}); e != nil {
		t.Fatal(e)
	}
	restored, e := s.Undo()
	if e != nil {
		t.Fatal(e)
	}
	text, e := s.Script(restored)
	if e != nil || text != "old bytes\n" {
		t.Fatalf("undo editor=%q %v", text, e)
	}
}
