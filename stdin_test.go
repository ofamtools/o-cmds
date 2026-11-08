package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func inputFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input with spaces;literal.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStdinFileHistoryAndRerun(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "consume", "printf '%s:' \"$1\"; cat")
	text := "binary\xff\x00input\n"
	path := inputFile(t, text)
	out, err := invoke(t, s, "ignored", "cmd", "run", c.Name, "--stdin", path, "arg")
	if err != nil || out != "arg:"+text {
		t.Fatalf("run %q %v", out, err)
	}
	r, err := s.ReadRun("1")
	if err != nil || r.Stdin.Kind != "file" || r.Stdin.Path != path || r.Stdin.Bytes != int64(len(text)) || r.Stdin.SHA != fmt.Sprintf("%x", sha256.Sum256([]byte(text))) || r.Input != text || r.InputBase64 == "" {
		t.Fatalf("history %+v %v", r, err)
	}
	got, err := invoke(t, s, "", "job", "in", "1")
	if err != nil || got != text {
		t.Fatalf("raw %q %v", got, err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = invoke(t, s, "inherited", "job", "rerun", "1")
	if err != nil || got != "arg:changed" {
		t.Fatalf("file rerun %q %v", got, err)
	}
	// History's raw input remains the original bytes after the source changes.
	got, err = invoke(t, s, "", "job", "in", "1")
	if err != nil || got != text {
		t.Fatalf("snapshot changed %q %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = invoke(t, s, "", "job", "rerun", "1"); err == nil {
		t.Fatal("missing original file silently used inherited stdin")
	}
	a, _, _ := historyApp(t, s)
	b, _ := json.Marshal(a.callTool("job_show", json.RawMessage(`{"job":"1"}`), "test"))
	if !strings.Contains(string(b), "stdin") || !strings.Contains(string(b), "input_base64") || !strings.Contains(string(b), r.Stdin.SHA) {
		t.Fatal(string(b))
	}
}

func TestStdinEditedFakeEditorAndReuse(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "edited", "cat")
	t.Setenv("EDITOR", "false")
	// VISUAL wins, and its arguments are kept; the path is a separate argument.
	t.Setenv("VISUAL", `sh -c 'test ! -s "$1" && printf "edited input\n" > "$1"' fake-editor`)
	out, err := invoke(t, s, "keyboard", "cmd", "run", c.Name, "--edit-stdin")
	if err != nil || out != "edited input\n" {
		t.Fatalf("edited %q %v", out, err)
	}
	r, err := s.ReadRun("1")
	if err != nil || r.Stdin.Kind != "edited" || r.Input != out || r.Stdin.Path != s.runPath(r.Run, ".in") {
		t.Fatalf("history %+v %v", r, err)
	}
	for _, path := range []string{r.Stdin.Path, filepath.Dir(r.Stdin.Path)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("permissions %s %v", path, info.Mode())
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, "tmp", "*"))
	if len(files) != 0 {
		t.Fatal("temp input leaked", files)
	}
	t.Setenv("VISUAL", "false")
	got, err := invoke(t, s, "wrong", "job", "rerun", "1")
	if err != nil || got != out {
		t.Fatalf("edited rerun %q %v", got, err)
	}
	r2, err := s.ReadRun("2")
	if err != nil || r2.Stdin.Kind != "edited" || r2.Stdin.SHA != r.Stdin.SHA || r2.Stdin.Path == r.Stdin.Path {
		t.Fatalf("rerun source %+v %v", r2, err)
	}
	if err := s.DeleteRun(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Stdin.Path); !os.IsNotExist(err) {
		t.Fatal("input not deleted", err)
	}
	got, err = invoke(t, s, "", "job", "rerun", r2.ID)
	if err != nil || got != out {
		t.Fatalf("rerun depended on deleted parent %q %v", got, err)
	}
}

func TestStdinCancellationAndInvalidSources(t *testing.T) {
	for _, tc := range []struct{ name, editor, want string }{
		{"empty", "true", "buffer is empty"},
		{"failed", `sh -c 'exit 7'`, "editor failed"},
		{"failed with content", `sh -c 'printf retained > "$1"; exit 7' fake-editor`, "editor failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			c := addCommand(t, s, "cancel", "cat")
			t.Setenv("VISUAL", tc.editor)
			_, err := invoke(t, s, "", "cmd", "run", c.Name, "--edit-stdin")
			if tc.name == "empty" {
				if err != nil {
					t.Fatalf("empty edit is a quiet cancel, got %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("cancel %v", err)
			}
			rows, err := s.Runs("")
			if err != nil || len(rows) != 0 {
				t.Fatalf("cancel recorded %+v %v", rows, err)
			}
			files, _ := filepath.Glob(filepath.Join(s.Dir, "tmp", "*"))
			if len(files) != 0 {
				t.Fatal("temp leaked", files)
			}
		})
	}
	s := fixture(t)
	c := addCommand(t, s, "invalid", "cat")
	for _, args := range [][]string{{"--stdin", t.TempDir()}, {"--stdin", filepath.Join(t.TempDir(), "missing")}, {"--stdin", inputFile(t, "x"), "--edit-stdin"}} {
		words := append([]string{"cmd", "run", c.Name}, args...)
		if _, err := invoke(t, s, "", words...); err == nil {
			t.Fatal("accepted", args)
		}
	}
}

func TestStdinRetentionCapOffAndInherited(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "caps", "cat")
	a, out, _ := historyApp(t, s)
	t.Setenv("VISUAL", `sh -c 'printf abcdef > "$1"' fake-editor`)
	if err := a.r.Settings().Set("jobs.max-bytes", 3); err != nil {
		t.Fatal(err)
	}
	if err := a.r.Settings().Set("jobs.keep", 1); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(a, []string{"cmd", "run", c.Name, "--edit-stdin"}); err != nil {
		t.Fatal(err)
	}
	r, err := s.ReadRun("1")
	if err != nil || r.Input != "abc" || r.Stdin.Bytes != 6 || !r.Stdin.Truncated || out.String() != "abcdef" {
		t.Fatalf("cap %+v %v out=%q", r, err, out)
	}
	if err := dispatch(a, []string{"job", "rerun", "1"}); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatal("replayed partial input", err)
	}
	a.in = strings.NewReader("input\n")
	if err := dispatch(a, []string{"cmd", "run", c.Name}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Stdin.Path); !os.IsNotExist(err) {
		t.Fatal("retention leaked input", err)
	}
	latest, err := s.ReadRun("1")
	if err != nil || latest.Stdin.Kind != "none" || latest.Input != "" || latest.Stdout != "inp" {
		t.Fatalf("inherited %+v %v", latest, err)
	}
	if err := a.r.Settings().Set("jobs.keep", 0); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := dispatch(a, []string{"cmd", "run", c.Name, "--edit-stdin"}); err != nil || out.String() != "abcdef" {
		t.Fatalf("recording off %q %v", out, err)
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, "tmp", "*"))
	if len(files) != 0 {
		t.Fatal("off leaked temp", files)
	}
}

func TestStdinEditorPermissionAndFallback(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "permissions", "cat")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "fake-editor --wait")
	a, _, _ := historyApp(t, s)
	calls := 0
	a.run = func(cmd *exec.Cmd) error {
		calls++
		if calls == 1 {
			if !strings.Contains(cmd.Args[2], "fake-editor --wait") {
				t.Fatal(cmd.Args)
			}
			path := cmd.Args[len(cmd.Args)-1]
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 0 || filepath.Dir(path) != filepath.Join(s.Dir, "tmp") {
				t.Fatalf("buffer %v %v", info, err)
			}
			return os.WriteFile(path, []byte("private"), 0o600)
		}
		return cmd.Run()
	}
	if err := dispatch(a, []string{"cmd", "run", c.Name, "--edit-stdin"}); err != nil || calls != 2 {
		t.Fatalf("runner %v calls=%d", err, calls)
	}
	t.Setenv("EDITOR", "")
	if cmd := editorCommand(background(), "file"); !strings.Contains(cmd.Args[2], "exec vi ") {
		t.Fatal(cmd.Args)
	}
}

func TestStdinCatalogAndMCPBoundedInput(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "catalog-input", "cat >/dev/null")
	a, _, _ := historyApp(t, s)
	path := inputFile(t, strings.Repeat("x", 65*1024))
	q := ochazuke.Do("cmd.run", ochazuke.Arguments{"target": c.ID, "stdin": path}).Request
	if _, err := a.r.Execute(q); err != nil {
		t.Fatal(err)
	}
	r, err := s.ReadRun("1")
	if err != nil || len(r.Input) != 65*1024 {
		t.Fatalf("input length %d %v", len(r.Input), err)
	}
	b, _ := json.Marshal(a.callTool("job_show", json.RawMessage(`{"job":"1"}`), "test"))
	if len(b) > 8000 || !strings.Contains(string(b), r.Stdin.SHA) {
		t.Fatalf("MCP omitted metadata or leaked large input: %d", len(b))
	}
}

func TestStdinLatestRerunAndBindings(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "latest", "cat; exit 7")
	path := inputFile(t, "same input")
	if _, err := invoke(t, s, "", "cmd", "run", c.Name, "--stdin", path); err == nil {
		t.Fatal("expected script failure")
	}
	out, err := invoke(t, s, "wrong", "cmd", "rerun", c.Name)
	if out != "same input" || err == nil {
		t.Fatalf("latest rerun output=%q err=%v", out, err)
	}
	a, _, _ := historyApp(t, s)
	if _, err := mountPage(a); err != nil {
		t.Fatal(err)
	}
	keys := func() map[string]int {
		keys := map[string]int{}
		for _, row := range ochazuke.BindingHelp(a.r, a.r.FocusID()) {
			keys[row.Key]++
		}
		return keys
	}
	for _, key := range []string{"g i", "g e", "g r"} {
		if keys()[key] != 1 {
			t.Fatalf("missing or clashing hint %q: %v", key, keys())
		}
	}
	if err := a.r.Settings().Set("view.stdin", false); err != nil {
		t.Fatal(err)
	}
	if keys()["g i"] != 0 || keys()["g e"] != 0 {
		t.Fatal("disabled stdin hints still visible", keys())
	}
	if err := a.r.Settings().Set("view.jobs", false); err != nil {
		t.Fatal(err)
	}
	if keys()["g r"] != 0 {
		t.Fatal("disabled rerun hint still visible")
	}
}

func TestStdinRelativePathAndRecentSources(t *testing.T) {
	s := fixture(t)
	base := t.TempDir()
	t.Chdir(base)
	if err := os.WriteFile("input.txt", []byte("relative"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := s.Add(Add{Name: "relative", Script: "cat", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, s, "", "cmd", "run", c.Name, "--stdin", "input.txt")
	if err != nil || out != "relative" {
		t.Fatalf("relative %q %v", out, err)
	}
	a, _, _ := historyApp(t, s)
	paths := a.recentInputs(ochazuke.PathPick{})
	if len(paths) != 1 || paths[0] != filepath.Join(base, "input.txt") {
		t.Fatal(paths)
	}
	out, err = invoke(t, s, "", "cmd", "run", "--json", `{"target":"relative","stdin":"input.txt"}`)
	if err != nil || out != "relative" {
		t.Fatalf("host stdin %q %v", out, err)
	}
	for _, words := range [][]string{{"job", "rerun"}, {"cmd", "rerun"}, {"cmd", "rerun", "relative", "extra"}} {
		if _, err := invoke(t, s, "", words...); err == nil {
			t.Fatal("missing/extra target accepted", words)
		}
	}
}
