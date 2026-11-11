package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestScriptImportExportAndDuplicate(t *testing.T) {
	s := fixture(t)
	path := filepath.Join(t.TempDir(), "source script.sh")
	const script = "#!/bin/sh\nprintf '%s\\n' 'never run during import'\n"
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, s, "", "cmd", "import", path, "--cwd", filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var imported Command
	if err := json.Unmarshal([]byte(out), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Name != "source script" || imported.SourcePath != path || imported.Source != "file" {
		t.Fatalf("provenance: %+v", imported)
	}
	destination := filepath.Join(t.TempDir(), "export.sh")
	if _, err := invoke(t, s, "", "cmd", "export", imported.ID, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, s, "", "cmd", "export", imported.ID, destination, "--append"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != script+script {
		t.Fatalf("export: %q %v", data, err)
	}
	for _, args := range [][]string{{"cmd", "export", imported.ID, imported.Path}, {"cmd", "export", imported.ID, imported.Path, "--append"}} {
		if _, err := invoke(t, s, "", args...); err == nil {
			t.Fatal("export overwrote or appended its source")
		}
	}
	if got, err := s.Script(imported); err != nil || got != script {
		t.Fatalf("source changed: %q %v", got, err)
	}
	if got, err := invoke(t, s, "", "cmd", "export", imported.ID, "-"); err != nil || got != script {
		t.Fatalf("stdout: %q %v", got, err)
	}
	if _, err := invoke(t, s, "", "cmd", "duplicate", imported.ID, "copy"); err != nil {
		t.Fatal(err)
	}
	copy, err := s.Resolve("copy", false)
	if err != nil || copy.ID == imported.ID || copy.Cwd != imported.Cwd {
		t.Fatalf("copy: %+v %v", copy, err)
	}
	if got, err := s.Script(copy); err != nil || got != script {
		t.Fatalf("copied script: %q %v", got, err)
	}
	if _, err := invoke(t, s, "printf stdin\n", "cmd", "import", "-", "--name", "piped"); err != nil {
		t.Fatal(err)
	}
	if got, err := invoke(t, s, "", "cmd", "show-script", "piped"); err != nil || got != "printf stdin\n" {
		t.Fatalf("stdin import: %q %v", got, err)
	}
}

func TestUnsavedLineJobRerunAndSave(t *testing.T) {
	s := fixture(t)
	input := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(input, []byte("input bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const line = "cat; printf 'error bytes\\n' >&2; exit 7"
	out, err := invoke(t, s, "", "cmd", "run-line", "--line", line, "--cwd", filepath.Dir(input), "--stdin", input)
	if err != nil {
		t.Fatal(err)
	}
	var job RunOutput
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatal(err)
	}
	if job.Script == nil || *job.Script != line || job.CommandID != "" || job.Ended == nil || job.ExitCode == nil || *job.ExitCode != 7 || job.Stdout != "input bytes\n" || job.Stderr != "error bytes\n" || job.Stdin.Path != input {
		t.Fatalf("captured job: %+v", job)
	}
	commands, err := s.List()
	if err != nil || len(commands) != 0 {
		t.Fatalf("run-line saved a command: %v %v", commands, err)
	}
	var streamed bytes.Buffer
	a, err := newApp(s, strings.NewReader(""), &streamed, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.rerunRecorded(context.Background(), job); err == nil {
		t.Fatal("rerun lost exit 7")
	}
	if streamed.String() != "input bytes\n" {
		t.Fatalf("rerun input: %q", streamed.String())
	}
	if _, err := invoke(t, s, "", "job", "save", job.ID, "saved-line"); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Resolve("saved-line", false)
	if err != nil || saved.Cwd != job.Cwd || saved.Source != "job:"+job.ID {
		t.Fatalf("saved provenance: %+v %v", saved, err)
	}
	if text, err := s.Script(saved); err != nil || text != line {
		t.Fatalf("saved script: %q %v", text, err)
	}
}

func TestLineJobVisibleWhileRunningAndRecordingCannotBeDisabled(t *testing.T) {
	s := fixture(t)
	a, err := newApp(s, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.r.Settings().Set("jobs.keep", 0); err != nil {
		t.Fatal(err)
	}
	a.run = func(cmd *exec.Cmd) error {
		runs, err := s.Runs("")
		if err != nil || len(runs) != 1 || runs[0].Ended != nil {
			t.Fatalf("running job absent: %+v %v", runs, err)
		}
		thing, err := a.jobThing(runs[0])
		if err != nil || thing["status"] != "running" || runStatus(runs[0]) != "running" {
			t.Fatalf("running state: %v %v", thing, err)
		}
		_, err = fmt.Fprint(cmd.Stdout, "captured")
		return err
	}
	job, err := a.captureLine(context.Background(), "printf captured", "", nil, "", "line")
	if err != nil || job.Stdout != "captured" || job.Ended == nil {
		t.Fatalf("finished job: %+v %v", job, err)
	}
}

func TestImportCapturedCommandOutput(t *testing.T) {
	s := fixture(t)
	a, err := newApp(s, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	value, err := a.importScript(ochazuke.Ctx{Args: ochazuke.Arguments{"command": "printf 'printf generated\\n'", "name": "generated"}}).Await(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	row := value.(Command)
	if !strings.HasPrefix(row.Source, "job:") {
		t.Fatalf("no producer provenance: %+v", row)
	}
	if text, err := s.Script(row); err != nil || text != "printf generated\n" {
		t.Fatalf("imported output: %q %v", text, err)
	}
}

func TestJobOutputFilesClipboardAndRegister(t *testing.T) {
	s := fixture(t)
	line := "unused fixture"
	r := Run{ID: "output-fixture", Script: &line, CommandName: line, Stdin: StdinSource{Kind: "none"}}
	if err := s.locked(func() error { return s.writeRun(r, []byte{0, 255, '\n'}, []byte("error bytes")) }); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "out")
	if _, err := invoke(t, s, "", "job", "write", r.ID, path); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, s, "", "job", "write", r.ID, path, "--stream", "stderr", "--append"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, append([]byte{0, 255, '\n'}, []byte("error bytes")...)) {
		t.Fatalf("binary output: %v %v", data, err)
	}
	if out, err := invoke(t, s, "", "job", "write", r.ID, "-"); err != nil || !bytes.Equal([]byte(out), []byte{0, 255, '\n'}) {
		t.Fatalf("binary stdout: %v %v", []byte(out), err)
	}
	a, err := newApp(s, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var clipped string
	a.run = func(cmd *exec.Cmd) error {
		data, err := io.ReadAll(cmd.Stdin)
		clipped = string(data)
		return err
	}
	t.Setenv("O_CMDS_CLIPBOARD", "fake-clipboard")
	if _, err := a.writeJob(ochazuke.Ctx{Args: ochazuke.Arguments{"job": r.ID, "stream": "stderr", "clipboard": true}}).Await(context.Background()); err != nil || clipped != "error bytes" {
		t.Fatalf("clipboard: %q %v", clipped, err)
	}
	if _, err := a.writeJob(ochazuke.Ctx{Args: ochazuke.Arguments{"job": r.ID, "clipboard": true}}).Await(context.Background()); err == nil {
		t.Fatal("binary clipboard silently changed bytes")
	}
	if err := dispatch(a, []string{"job", "write", r.ID, "--stream", "stderr", "--register", "a"}); err != nil {
		t.Fatal(err)
	}
	if got := a.r.Registers().Named["a"].Yank.Text; got != "error bytes" {
		t.Fatalf("register: %q", got)
	}
	if err := dispatch(a, []string{"job", "write", r.ID, "--stream", "stderr", "--register", "a", "--append"}); err != nil {
		t.Fatal(err)
	}
	if got := a.r.Registers().Named["a"].Yank.Text; got != "error byteserror bytes" {
		t.Fatalf("register append: %q", got)
	}
}

func TestOpeningScriptAndJobNeverExecutesTheirContents(t *testing.T) {
	s := fixture(t)
	row := addCommand(t, s, "script", "never execute")
	a, err := newApp(s, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	a.run = func(*exec.Cmd) error { return errors.New("opening tried to execute a script") }
	var paths [][]string
	a.r.SetOpener(ochazuke.Opener{GOOS: "darwin", Start: func(name string, args ...string) error {
		paths = append(paths, append([]string{name}, args...))
		return nil
	}})
	if _, err := a.r.Execute(ochazuke.Request{Noun: "cmd", Verb: "open", Args: ochazuke.Arguments{"id": row.ID}}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0][len(paths[0])-1] != row.Path {
		t.Fatalf("script opener: %v", paths)
	}
	line := "fixture"
	r := Run{ID: "opener-fixture", Script: &line, CommandName: line, Stdin: StdinSource{Kind: "none"}}
	if err := s.locked(func() error { return s.writeRun(r, []byte("output"), nil) }); err != nil {
		t.Fatal(err)
	}
	if err := dispatch(a, []string{"job", "open", r.ID, "--with", "TextEdit"}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || strings.Join(paths[1], "|") != "open|-a|TextEdit|--|"+s.runPath(r, ".out") {
		t.Fatalf("output opener: %v", paths)
	}
}

func TestStoredShowsUseStandardFormsAndKeepViewers(t *testing.T) {
	s := fixture(t)
	row := addCommand(t, s, "form-script", "printf unchanged")
	a, err := newApp(s, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	model := ochazuke.NewModel(a.r)
	model.Init()
	defer a.r.Sourced("palette")()
	if _, err := a.r.Execute(ochazuke.Request{Noun: "cmd", Verb: "show", Args: ochazuke.Arguments{"id": row.ID}}); err != nil {
		t.Fatal(err)
	}
	if a.r.Page != ochazuke.IndexPageID("cmd") || a.r.ShowFormOf(a.r.Page+"/form") == nil {
		t.Fatalf("show did not open the record form: %s", a.r.Page)
	}
	if out, err := invoke(t, s, "", "cmd", "show-script", row.ID); err != nil || out != "printf unchanged" {
		t.Fatalf("viewer changed: %q %v", out, err)
	}
}

func TestSetupActionsResolveInstructionFilesNotCommands(t *testing.T) {
	s := fixture(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(os.Getenv("HOME"), ".claude"))
	a, err := newApp(s, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	var items []struct{ ID string }
	if err := json.Unmarshal(setupItems, &items); err != nil || len(items) == 0 {
		t.Fatalf("setup items: %v", err)
	}
	_, available := a.r.Operation("cmd.setup-reveal")
	if !available {
		t.Skip("typed setup path verbs require the shared ochazuke setup metadata fix")
	}
	var output bytes.Buffer
	a.out = &output
	if err := dispatch(a, []string{"cmd", "setup-get", items[0].ID}); err != nil {
		t.Fatal(err)
	}
	var record struct{ Path string }
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(record.Path, os.Getenv("HOME")+string(os.PathSeparator)) {
		t.Fatalf("instruction file outside fixture HOME: %q", record.Path)
	}
	output.Reset()
	if err := dispatch(a, []string{"cmd", "setup-show", items[0].ID}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(record.Path)) {
		t.Fatalf("show lost instruction destination: %s", output.String())
	}
	var opened string
	a.r.SetOpener(ochazuke.Opener{GOOS: "darwin", Start: func(_ string, args ...string) error {
		opened = args[len(args)-1]
		return nil
	}})
	if err := dispatch(a, []string{"cmd", "setup-reveal", items[0].ID}); err != nil {
		t.Fatal(err)
	}
	if opened != record.Path {
		t.Fatalf("reveal opened %q, want instruction file %q", opened, record.Path)
	}
}
