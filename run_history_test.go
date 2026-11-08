package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

func historyApp(t *testing.T, s *Store) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	a, e := newApp(s, strings.NewReader("input\n"), &out, &errOut)
	if e != nil {
		t.Fatal(e)
	}
	return a, &out, &errOut
}

func TestRunHistoryExecution(t *testing.T) {
	for _, tc := range []struct {
		name, suffix, signal string
		code                 int
	}{
		{"success", "", "", 0}, {"failure", "exit 7", "", 7}, {"signal", "kill -TERM $$", "terminated", 143},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			cwd := t.TempDir()
			script := "printf '%s:%s\\n' \"$PWD\" \"$1\"; cat; printf 'separate error\\n' >&2; " + tc.suffix + "\n"
			c, e := s.Add(Add{Name: tc.name, Script: script, Cwd: cwd})
			if e != nil {
				t.Fatal(e)
			}
			a, out, errOut := historyApp(t, s)
			e = dispatch(a, []string{"cmd", "run", c.Name, "--", "two words"})
			if oden.ExitCode(e) != tc.code {
				t.Fatalf("status %v", e)
			}
			r, e := s.ReadRun("1")
			if e != nil {
				t.Fatal(e)
			}
			if r.Stdout != out.String() || r.Stderr != errOut.String() || r.Stderr != "separate error\n" || r.Stdout != cwd+":two words\ninput\n" {
				t.Fatalf("streams %+v, %q %q", r, out, errOut)
			}
			if r.CommandID != c.ID || r.CommandName != c.Name || r.Cwd != cwd || len(r.Args) != 1 || r.Args[0] != "two words" || r.ScriptHash != fmt.Sprintf("%x", sha256.Sum256([]byte(script))) {
				t.Fatalf("identity %+v", r)
			}
			if r.Ended == nil || r.Ended.Before(r.Started) || r.Duration <= 0 || r.Source != "cli" || r.Author.Name != "owner" {
				t.Fatalf("timing/author %+v", r)
			}
			if r.Signal != tc.signal {
				t.Fatalf("signal %+v", r)
			}
			if tc.signal == "" && (r.ExitCode == nil || *r.ExitCode != tc.code) {
				t.Fatalf("exit %+v", r)
			}
			if tc.signal != "" && r.ExitCode != nil {
				t.Fatalf("signal has exit code %+v", r)
			}
			for _, ext := range []string{".json", ".out", ".err"} {
				info, e := os.Stat(s.runPath(r.Run, ext))
				if e != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("permission %s %v %v", ext, info, e)
				}
			}
			for _, dir := range []string{filepath.Join(s.Dir, "runs"), filepath.Dir(s.runPath(r.Run, ".json"))} {
				info, e := os.Stat(dir)
				if e != nil || info.Mode().Perm() != 0o700 {
					t.Fatalf("directory permission %v %v", info, e)
				}
			}
		})
	}
	t.Run("start failure", func(t *testing.T) {
		s := fixture(t)
		c, e := s.Add(Add{Name: "missing-cwd", Script: "printf never", Cwd: filepath.Join(t.TempDir(), "missing")})
		if e != nil {
			t.Fatal(e)
		}
		a, _, _ := historyApp(t, s)
		if dispatch(a, []string{"cmd", "run", c.Name}) == nil {
			t.Fatal("expected start failure")
		}
		r, e := s.ReadRun("1")
		if e != nil || r.Ended == nil || r.ExitCode != nil || r.Error == "" {
			t.Fatalf("start failure %+v %v", r, e)
		}
	})
}

func TestRunHistoryTruncationRetentionOffAndUndo(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "capture", "printf abcdef; printf uvwxyz >&2")
	a, out, errOut := historyApp(t, s)
	for name, value := range map[string]int{"runs.keep": 2, "runs.max-bytes": 3} {
		if e := a.r.Settings().Set(name, value); e != nil {
			t.Fatal(e)
		}
	}
	for range 3 {
		if e := dispatch(a, []string{"cmd", "run", c.Name}); e != nil {
			t.Fatal(e)
		}
	}
	rows, e := s.Runs(c.ID)
	if e != nil || len(rows) != 2 {
		t.Fatalf("retention %v %v", rows, e)
	}
	r, e := s.ReadRun("2")
	if e != nil || r.Stdout != "abc" || r.Stderr != "uvw" || !r.StdoutCut || !r.StderrCut || r.StdoutBytes != 6 || r.StderrBytes != 6 {
		t.Fatalf("cap %+v %v", r, e)
	}
	if out.String() != strings.Repeat("abcdef", 3) || errOut.String() != strings.Repeat("uvwxyz", 3) {
		t.Fatal("live output truncated")
	}
	files, e := filepath.Glob(filepath.Join(s.Dir, "runs", c.ID, "*"))
	if e != nil || len(files) != 6 {
		t.Fatalf("pruned stream files %v %v", files, e)
	}
	if _, e := s.Change(c.ID, func(c *Command, _ []Command) { c.Deleted = true }); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadRun(r.ID); e != nil {
		t.Fatal("delete removed run", e)
	}
	if _, e := s.Undo(); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Runs(c.ID)
	if len(rows) != 2 {
		t.Fatal("undo lost runs")
	}
	if e := a.r.Settings().Set("runs.keep", 0); e != nil {
		t.Fatal(e)
	}
	if e := dispatch(a, []string{"cmd", "run", c.Name}); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Runs(c.ID)
	if len(rows) != 2 {
		t.Fatal("off should retain existing history without adding")
	}
	if e := s.ClearRuns(c.ID); e != nil {
		t.Fatal(e)
	}
	if e := dispatch(a, []string{"cmd", "run", c.Name}); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Runs("")
	if len(rows) != 0 {
		t.Fatal("keep=0 recorded")
	}
	if e := a.r.Settings().Set("runs.keep", -1); e == nil {
		t.Fatal("accepted negative retention")
	}
	if e := a.r.Settings().Set("runs.max-bytes", -1); e == nil {
		t.Fatal("accepted negative cap")
	}
}

func TestRunHistoryCLIAndMCP(t *testing.T) {
	s := fixture(t)
	cwd := t.TempDir()
	c, e := s.Add(Add{Name: "replay", Script: "printf '%s:%s' \"$PWD\" \"$1\"; printf err >&2", Cwd: cwd})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := invoke(t, s, "", "cmd", "run", c.Name, "--", "original arg"); e != nil {
		t.Fatal(e)
	}
	r, _ := s.ReadRun("1")
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"cmd", "jobs", "rep"}, "exit 0"},
		{[]string{"job", "list", "replay"}, r.ID},
		{[]string{"job", "show", "1"}, "--- stderr ---\nerr"},
		{[]string{"job", "out", r.ID[:25]}, cwd + ":original arg"},
		{[]string{"job", "err", "run/" + r.ID}, "err"},
	} {
		out, e := invoke(t, s, "", tc.words...)
		if e != nil || !strings.Contains(out, tc.want) {
			t.Fatalf("%v = %q %v", tc.words, out, e)
		}
	}
	// Current command cwd may change; rerun must use the run's cwd and arguments.
	if _, e := s.Change(c.ID, func(c *Command, _ []Command) { c.Cwd = t.TempDir() }); e != nil {
		t.Fatal(e)
	}
	out, e := invoke(t, s, "", "job", "rerun", "1")
	if e != nil || out != cwd+":original arg" {
		t.Fatalf("rerun %q %v", out, e)
	}
	a, _, _ := historyApp(t, s)
	for _, tc := range []struct{ tool, args, want string }{
		{"job_list", `{"command":"rep"}`, r.ID},
		{"job_show", `{"job":"1"}`, "original arg"},
		{"run_rerun", `{"job":"1"}`, "unknown tool"},
		{"run_delete", `{"job":"1"}`, "unknown tool"},
	} {
		b, _ := json.Marshal(a.callTool(tc.tool, json.RawMessage(tc.args), "agent"))
		if !strings.Contains(string(b), tc.want) {
			t.Fatalf("%s: %s", tc.tool, b)
		}
	}
	if _, e := invoke(t, s, "", "job", "delete", "1"); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.Runs("")
	if len(rows) != 1 {
		t.Fatal("delete", rows)
	}
	if _, e := invoke(t, s, "", "cmd", "jobs-clear", "replay"); e != nil {
		t.Fatal(e)
	}
	rows, _ = s.Runs("")
	if len(rows) != 0 {
		t.Fatal("clear", rows)
	}
}

func TestRunHistoryForegroundAndInjectedRunner(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "tui-run", "printf foreground; printf error >&2")
	a, _, _ := historyApp(t, s)
	p, e := mountPage(a)
	if e != nil {
		t.Fatal(e)
	}
	p.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd { return func() tea.Msg { return msg.Done(msg.Cmd.Run()) } }
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		deliver(t, p, cmd())
	}
	r, e := s.ReadRun("1")
	if e != nil || r.Source != "tui" || r.Ended == nil || r.Stdout != "foreground" || r.Stderr != "error" {
		t.Fatalf("foreground %+v %v", r, e)
	}
	a.tui = false
	a.run = func(cmd *exec.Cmd) error {
		if cmd.Args[1] != c.Path {
			t.Fatal(cmd.Args)
		}
		_, _ = io.WriteString(cmd.Stdout, "injected")
		return errors.New("fake start failure")
	}
	if e := dispatch(a, []string{"cmd", "run", c.Name}); e == nil {
		t.Fatal("fake error lost")
	}
	r, e = s.ReadRun("2")
	if e != nil || r.Stdout != "injected" || r.Error != "fake start failure" {
		t.Fatalf("injection %+v %v", r, e)
	}
}

func TestRunHistoryTTYStreams(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "terminal", "test -t 1 && printf '\\033[32mtty-out\\033[0m'; test -t 2 && printf tty-err >&2; stty size <&1 2>/dev/null")
	a, _, _ := historyApp(t, s)
	var slaves []*os.File
	var masters []*os.File
	for range 2 {
		m, slave, e := pty.Open()
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = m.Close(); _ = slave.Close() })
		if _, e := term.MakeRaw(slave.Fd()); e != nil {
			t.Fatal(e)
		}
		if e := pty.Setsize(slave, &pty.Winsize{Rows: 37, Cols: 91}); e != nil {
			t.Fatal(e)
		}
		slaves, masters = append(slaves, slave), append(masters, m)
	}
	a.out, a.errOut = slaves[0], slaves[1]
	if e := dispatch(a, []string{"cmd", "run", c.Name}); e != nil {
		t.Fatal(e)
	}
	r, e := s.ReadRun("1")
	if e != nil || r.Stdout != "\x1b[32mtty-out\x1b[0m37 91\n" || r.Stderr != "tty-err" || r.CaptureError != "" {
		t.Fatalf("tty %+v %v", r, e)
	}
	for i, want := range []string{r.Stdout, r.Stderr} {
		buf := make([]byte, len(want))
		if _, e := io.ReadFull(masters[i], buf); e != nil || string(buf) != want {
			t.Fatalf("live tty %q %v", buf, e)
		}
	}
}

func TestRunHistoryBinaryAndZeroCap(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "bytes", "printf '\\377\\000A'; printf '\\376\\000B' >&2")
	if _, e := invoke(t, s, "", "cmd", "run", c.Name); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ stream, want string }{{"out", "\xff\x00A"}, {"err", "\xfe\x00B"}} {
		out, e := invoke(t, s, "", "job", tc.stream, "1")
		if e != nil || out != tc.want {
			t.Fatalf("raw %s %q %v", tc.stream, out, e)
		}
	}
	a, out, errOut := historyApp(t, s)
	if e := a.r.Settings().Set("runs.max-bytes", 0); e != nil {
		t.Fatal(e)
	}
	if e := dispatch(a, []string{"cmd", "run", c.Name}); e != nil {
		t.Fatal(e)
	}
	r, e := s.ReadRun("2")
	if e != nil || r.Stdout != "" || r.Stderr != "" || !r.StdoutCut || !r.StderrCut || r.StdoutBytes != 3 || r.StderrBytes != 3 || out.String() != "\xff\x00A" || errOut.String() != "\xfe\x00B" {
		t.Fatalf("zero cap %+v %v", r, e)
	}
}

func TestRunHistoryRerunFailureAndScriptChange(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "changing", "printf before")
	if _, e := invoke(t, s, "", "cmd", "run", c.Name); e != nil {
		t.Fatal(e)
	}
	before, _ := s.ReadRun("1")
	if e := os.WriteFile(c.Path, []byte("printf after; exit 7"), 0o600); e != nil {
		t.Fatal(e)
	}
	out, e := invoke(t, s, "", "job", "rerun", "1")
	if out != "after" || oden.ExitCode(e) != 7 {
		t.Fatalf("rerun failure %q %v (code %d)", out, e, oden.ExitCode(e))
	}
	after, e := s.ReadRun("2")
	if e != nil || before.ScriptHash == after.ScriptHash || *after.ExitCode != 7 {
		t.Fatalf("hash %+v %v", after, e)
	}
}

func TestRunHistoryActiveClearAndConcurrentCompletion(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "active", "")
	a, _, _ := historyApp(t, s)
	cmd := scriptCommand(background(), c, nil)
	finish, e := a.prepareRun(cmd, c, nil, "cli")
	if e != nil {
		t.Fatal(e)
	}
	active, e := s.ReadRun("1")
	if e != nil || active.Ended != nil || active.ExitCode != nil {
		t.Fatalf("unfinished %+v %v", active, e)
	}
	if e := s.ClearRuns(c.ID); e != nil {
		t.Fatal(e)
	}
	if e := finish(nil); e != nil {
		t.Fatal(e)
	}
	rows, _ := s.Runs("")
	if len(rows) != 0 {
		t.Fatal("completion resurrected cleared run")
	}
	// Concurrent completions share the process lock and enforce the same cap.
	if e := a.r.Settings().Set("runs.keep", 2); e != nil {
		t.Fatal(e)
	}
	finishes := []func(error) error{}
	for range 4 {
		cmd := scriptCommand(background(), c, nil)
		finish, e := a.prepareRun(cmd, c, nil, "cli")
		if e != nil {
			t.Fatal(e)
		}
		finishes = append(finishes, finish)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(finishes))
	for _, finish := range finishes {
		wg.Go(func() { errCh <- finish(nil) })
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Fatal(e)
		}
	}
	rows, e = s.Runs("")
	if e != nil || len(rows) != 2 {
		t.Fatalf("concurrent retention %+v %v", rows, e)
	}
}

func TestRunHistoryBoundedPTYDrain(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "background", "")
	a, _, _ := historyApp(t, s)
	m, slave, e := pty.Open()
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	defer slave.Close()
	a.out = slave
	a.run = func(cmd *exec.Cmd) error {
		stream := cmd.Stdout.(*os.File)
		fd, e := unix.FcntlInt(stream.Fd(), unix.F_DUPFD_CLOEXEC, 0)
		if e != nil {
			t.Fatal(e)
		}
		held := os.NewFile(uintptr(fd), "fake-descendant-output")
		// Ensure a broken drain cannot hang the whole test suite.
		timer := time.AfterFunc(3*time.Second, func() { _ = held.Close() })
		t.Cleanup(func() { timer.Stop(); _ = held.Close() })
		_, e = io.WriteString(cmd.Stdout, "ready")
		return e
	}
	at := time.Now()
	if e := dispatch(a, []string{"cmd", "run", c.Name}); e != nil {
		t.Fatal(e)
	}
	if time.Since(at) >= 2*time.Second {
		t.Fatal("PTY drain did not stop promptly")
	}
	r, e := s.ReadRun("1")
	if e != nil || r.Stdout != "ready" || !strings.Contains(r.CaptureError, "timed out") {
		t.Fatalf("drain %+v %v", r, e)
	}
}
