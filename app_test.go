package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/widgets/table"
)

// invoke runs the CLI against store s and returns stdout or an error when
// exit code is non-zero.
func invoke(t *testing.T, s *Store, stdin string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	full := append([]string{"--data", s.Dir}, args...)
	code := run(full, strings.NewReader(stdin), &out, &errOut)
	if code != 0 {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = "exit " + string(rune('0'+code))
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

func TestCLIAddListShowRunRerunJobs(t *testing.T) {
	s := fixture(t)

	// add & new
	if out, err := invoke(t, s, "echo \"hi $1 yes=${OCHAZUKE_ASSUME_YES:-0}\"\n", "add", "--name", "greet", "--desc", "Say hi", "--tags", "demo,test"); err != nil {
		t.Fatalf("add: %v", err)
	} else if !strings.HasPrefix(out, "greet\t") {
		t.Fatalf("add output = %q", out)
	}
	if _, err := invoke(t, s, "", "new", "--name", "failer", "--script", "echo oops >&2; exit 7\n"); err != nil {
		t.Fatalf("new: %v", err)
	}

	// list (table & json)
	if out, err := invoke(t, s, "", "list"); err != nil || !strings.Contains(out, "greet") || !strings.Contains(out, "failer") {
		t.Fatalf("list table: out=%q err=%v", out, err)
	}
	if out, err := invoke(t, s, "", "list", "--format", "json"); err != nil {
		t.Fatalf("list json: %v", err)
	} else {
		var rows []listRow
		if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 {
			t.Fatalf("list json rows=%d err=%v", len(rows), err)
		}
	}

	// show
	if out, err := invoke(t, s, "", "show", "greet"); err != nil || !strings.Contains(out, "OCHAZUKE_ASSUME_YES") {
		t.Fatalf("show: out=%q err=%v", out, err)
	}

	// run with --assume-yes and args
	if out, err := invoke(t, s, "", "run", "greet", "--assume-yes", "--", "world"); err != nil || strings.TrimSpace(out) != "hi world yes=1" {
		t.Fatalf("run greet: out=%q err=%v", out, err)
	}

	// rerun repeats args and assume-yes
	if out, err := invoke(t, s, "", "rerun", "1"); err != nil || strings.TrimSpace(out) != "hi world yes=1" {
		t.Fatalf("rerun 1: out=%q err=%v", out, err)
	}

	// run preserves non-zero exit code
	var failOut, failErr bytes.Buffer
	code := run([]string{"--data", s.Dir, "run", "failer"}, strings.NewReader(""), &failOut, &failErr)
	if code != 7 || !strings.Contains(failErr.String(), "oops") {
		t.Fatalf("run failer: code=%d err=%q", code, failErr.String())
	}

	// jobs table & json
	if out, err := invoke(t, s, "", "jobs"); err != nil || !strings.Contains(out, "greet") || !strings.Contains(out, "exit 7") {
		t.Fatalf("jobs: out=%q err=%v", out, err)
	}
	if out, err := invoke(t, s, "", "jobs", "--format", "json"); err != nil {
		t.Fatalf("jobs json: %v", err)
	} else {
		var runs []runRow
		if err := json.Unmarshal([]byte(out), &runs); err != nil || len(runs) != 3 {
			t.Fatalf("jobs json runs=%d err=%v", len(runs), err)
		}
		if !runs[0].AssumeYes || !runs[1].AssumeYes {
			t.Fatalf("expected assume_yes persisted on run & rerun: %+v", runs)
		}
	}

	// --version
	if out, err := invoke(t, s, "", "--version"); err != nil || !strings.Contains(out, "o-cmds "+version) {
		t.Fatalf("--version: out=%q err=%v", out, err)
	}
}

func TestCatalogAndQueryProtocol(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "deploy", "echo deploying\n")
	if _, err := invoke(t, s, "", "run", "deploy"); err != nil {
		t.Fatalf("run deploy: %v", err)
	}

	// catalog prints a valid ext.Manifest declaring cmds.command and cmds.job
	catOut, err := invoke(t, s, "", "catalog")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var m extManifest
	if err := json.Unmarshal([]byte(catOut), &m); err != nil {
		t.Fatalf("catalog json: %v", err)
	}
	if m.Name != "o-cmds" || len(m.Schemas) != 2 {
		t.Fatalf("manifest=%+v", m)
	}
	if m.Schemas[0].Kind != "cmds.command" || m.Schemas[1].Kind != "cmds.job" {
		t.Fatalf("schema kinds=%q, %q", m.Schemas[0].Kind, m.Schemas[1].Kind)
	}
	if len(m.Commands) == 0 {
		t.Fatal("manifest has no commands")
	}

	// query prints {"items": [...], "total": n}
	qOut, err := invoke(t, s, "", "query")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var q extQueryResult
	if err := json.Unmarshal([]byte(qOut), &q); err != nil {
		t.Fatalf("query json: %v", err)
	}
	if q.Total != 2 || len(q.Items) != 2 {
		t.Fatalf("query total=%d len=%d", q.Total, len(q.Items))
	}
	qCmd, err := invoke(t, s, "", "query", "--kind", "cmds.command", "deploy")
	if err != nil {
		t.Fatalf("query filtered: %v", err)
	}
	var q2 extQueryResult
	if err := json.Unmarshal([]byte(qCmd), &q2); err != nil || q2.Total != 1 || q2.Items[0].Kind != "cmds.command" {
		t.Fatalf("query filtered=%+v err=%v", q2, err)
	}
}

func TestMCPServerLifecycleAndNoRun(t *testing.T) {
	s := fixture(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"test-client"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cmd_add","arguments":{"name":"mcp-cmd","desc":"from mcp","script":"echo hi\n"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"cmd_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"cmd_show","arguments":{"target":"mcp-cmd"}}}`,
	}, "\n") + "\n"

	out, err := invoke(t, s, input, "mcp")
	if err != nil {
		t.Fatalf("mcp: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 responses, got %d: %s", len(lines), out)
	}
	// tools/list must not expose any run tool.
	if strings.Contains(lines[1], `"cmd_run"`) || !strings.Contains(lines[1], `"cmd_add"`) {
		t.Fatalf("unexpected tools/list: %s", lines[1])
	}
	if !strings.Contains(lines[4], `echo hi`) {
		t.Fatalf("cmd_show missing script: %s", lines[4])
	}
}

func TestTUIWorkflowReadlineJobsAndCancel(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "alpha", "echo alpha-out\n")
	addCommand(t, s, "beta", "echo yes=${OCHAZUKE_ASSUME_YES:-0}\n")
	addCommand(t, s, "slow", "sleep 30\n")

	svc := newService(s, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, nil)
	env := newEnv(svc, Config{})
	env.Refresh = 0
	sh := newShell(env)
	d := newDriver(sh, 120, 32)
	app := func() App { return appOf(sh.State) }
	frame := func() string { return plain(sh.View().Content) }

	// Move to beta (index 1) and run with assume-yes.
	d.keys("g", "g", "j", "g", "Y")
	if len(app().Runs) != 1 || !app().Runs[0].AssumeYes {
		t.Fatalf("expected 1 assume-yes run, got %+v", app().Runs)
	}
	if !strings.Contains(frame(), "yes=1") {
		t.Fatalf("viewer missing output:\n%s", frame())
	}
	d.keys("esc")

	// Filter with Readline motions: type "alpx", ctrl+b, ctrl+d, verify match.
	d.keys("/")
	d.typeText("alpx")
	d.keys("ctrl+b", "ctrl+d")
	fa := app()
	if fa.Cmd.Grid.Query != "alp" || len(fa.Cmd.Grid.Items()) != 1 {
		t.Fatalf("readline filter query=%q items=%d", fa.Cmd.Grid.Query, len(fa.Cmd.Grid.Items()))
	}
	d.keys("esc")

	// Sort cycle and reverse.
	d.keys("s")
	if app().Cmd.Sort != 0 || app().Cmd.Grid.SortDir != table.SortAsc {
		t.Fatalf("sort=%d dir=%v", app().Cmd.Sort, app().Cmd.Grid.SortDir)
	}
	d.keys("S")
	if app().Cmd.Grid.SortDir != table.SortDesc {
		t.Fatalf("reverse sort dir=%v", app().Cmd.Grid.SortDir)
	}

	// Edit focused command in form with Readline.
	d.keys("g", "g", "e")
	if len(sh.State.Modals) != 1 || sh.State.Modals[0].ID != modalForm {
		t.Fatalf("edit form did not open: %+v", sh.State.Modals)
	}
	d.keys("tab", "ctrl+u")
	d.typeText("updated desc")
	d.keys("enter")
	if len(sh.State.Modals) != 0 {
		t.Fatalf("form did not close")
	}

	// Jobs view headers.
	d.keys("tab")
	jf := frame()
	for _, col := range []string{"#", "Exit", "Duration", "State"} {
		if !strings.Contains(jf, col) {
			t.Fatalf("Jobs table missing column %q:\n%s", col, jf)
		}
	}

	// Smoke test runs cleanly end-to-end.
	var smokeOut bytes.Buffer
	if code := runSmoke(&smokeOut); code != 0 {
		t.Fatalf("runSmoke failed (%d):\n%s", code, smokeOut.String())
	}
	// Cancel a running job via the engine without blocking.
	env2 := newEnv(svc, Config{})
	env2.Refresh = 0
	sh2 := newShell(env2)
	sh2.Engine.PrefixTimeout = 0
	sh2.MaxFPS = 0
	_, _ = sh2.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	_, _ = sh2.Update(keyMsg("G")) // last command = slow
	_, cmd := sh2.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("expected async run cmd for slow")
	}
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = cmd()
	}()
	// Wait briefly for the slot to be active, then cancel with C.
	time.Sleep(50 * time.Millisecond)
	_, cancelCmd := sh2.Update(keyMsg("C"))
	if cancelCmd != nil {
		msg := cancelCmd()
		_, _ = sh2.Update(msg)
	}
	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled slow job did not exit")
	}
}
