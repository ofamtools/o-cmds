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
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"ofam.dev/ochazuke/config"
)

const version = "0.2.0"

// Service is o-cmds' domain: the store, how runs are recorded, and the
// settings that shape both. The CLI, the MCP server and the TUI share it.
type Service struct {
	store       *Store
	in          io.Reader
	out, errOut io.Writer
	run         Runner

	keep, maxBytes int
	order          string // oldest-first or newest-first
	showDeleted    bool
	changed        func()
}

// defaultSettings are the recognized settings.json keys and their defaults.
var defaultSettings = map[string]any{
	"jobs.keep":         20,             // completed runs retained per command; 0 disables recording
	"jobs.max-bytes":    1024 * 1024,    // bytes retained per stdout/stderr/stdin stream
	"list.order":        "oldest-first", // oldest-first or newest-first (numbered targets follow it)
	"list.show-deleted": false,          // list deleted commands too (numbered "-")
	"list.refresh":      2.0,            // seconds between TUI reloads for agent additions; 0 disables
	"list.start-newest": true,           // start the TUI on the newest command
	"view.detail":       true,           // show the detail pane beside the list
	"view.job-columns":  true,           // show the Last and Runs columns
	"view.run-output":   true,           // open the output viewer when a captured TUI run ends
	"matchMode":         "substring",    // filter and palette match mode: substring, fuzzy or regex
	"detailRatio":       0.55,           // list share of the detail split, 0.2 to 0.8
	"stackWidth":        110,            // below this terminal width the detail pane stacks under the list
	"clipboard":         true,           // copy yanks to the system clipboard
}

func newService(s *Store, in io.Reader, out, errOut io.Writer, set *config.Settings) *Service {
	if set == nil {
		set, _ = config.LoadSettings(nil, defaultSettings)
	}
	a := &Service{store: s, in: in, out: out, errOut: errOut, run: func(c *exec.Cmd) error { return c.Run() }}
	a.keep = max(0, set.Int("jobs.keep", 20))
	a.maxBytes = max(0, set.Int("jobs.max-bytes", 1024*1024))
	a.order = set.String("list.order", "oldest-first")
	a.showDeleted = set.Bool("list.show-deleted", false)
	return a
}

func (a *Service) newestFirst() bool { return a.order == "newest-first" }

func (a *Service) refresh() {
	if a.changed != nil {
		a.changed()
	}
}

// resolve finds a command by name, displayed number, ID or unique prefix,
// numbering in the configured order.
func (a *Service) resolve(target string) (Command, error) {
	return a.store.Resolve(target, a.newestFirst())
}

// cmdRow is one listed command with what the list derives from it.
type cmdRow struct {
	Command
	Number  int // displayed number; 0 for a deleted command
	Script  string
	Status  commandStatus
	LastRun *Run
	Author  string
}

func authorLabel(c Command) string {
	author := c.Author.Name
	if c.Author.Harness != "" && c.Author.Harness != author {
		author += "/" + c.Author.Harness
	}
	if c.Author.Pane != "" {
		author += "@" + c.Author.Pane
	}
	return author
}

// rows lists the commands in the configured order, numbered as CLI targets
// are, keeping those whose name, description or tags contain filter.
func (a *Service) rows(filter string) ([]cmdRow, error) {
	cs, e := a.store.records(a.showDeleted)
	if e != nil {
		return nil, e
	}
	if a.newestFirst() {
		slices.Reverse(cs)
	}
	runs, err := a.store.Runs("")
	if err != nil {
		return nil, err
	}
	last := map[string]Run{}
	for _, r := range runs {
		last[r.CommandID] = r
	}
	filter = strings.ToLower(filter)
	out := []cmdRow{}
	number := 0
	for _, c := range cs {
		n := 0
		if !c.Deleted {
			number++
			n = number
		}
		if !strings.Contains(strings.ToLower(c.Name+" "+c.Description+" "+strings.Join(c.Tags, " ")), filter) {
			continue
		}
		text, e := a.store.Script(c)
		if e != nil {
			return nil, e
		}
		row := cmdRow{Command: c, Number: n, Script: text, Status: commandStatusOf(c, runs), Author: authorLabel(c)}
		if r, ok := last[c.ID]; ok {
			row.LastRun = &r
		}
		out = append(out, row)
	}
	return out, nil
}

// data is the row as a JSON object: the stored command plus what the list
// derives (script, status, runs, ages), the shape cmd_list has answered.
func (r cmdRow) data(now time.Time) map[string]any {
	encoded, _ := json.Marshal(r.Command)
	var data map[string]any
	_ = json.Unmarshal(encoded, &data)
	data["script"] = r.Script
	data["script_path"] = r.Path
	data["detail_md"] = scriptMarkdown(r.Script, r.Path)
	data["age"] = age(r.Created, now)
	data["number"] = r.Number
	data["author_label"] = r.Author
	if r.LastRun != nil {
		data["last_run"] = runSummary(*r.LastRun)
	}
	data["runs"], data["status"] = r.Status.Count, r.Status.Status
	if r.Status.Last != nil {
		data["last_run_at"] = r.Status.Last.Format(time.RFC3339)
		data["last_run_age"] = age(*r.Status.Last, now)
	}
	return data
}

// listRow is one cmd_list answer: the row as catalog hosts have read it.
type listRow struct {
	Noun  string         `json:"noun"`
	ID    string         `json:"id"`
	Label string         `json:"label"`
	Data  map[string]any `json:"data"`
}

func (a *Service) listRows(filter string) ([]listRow, error) {
	rows, err := a.rows(filter)
	if err != nil {
		return nil, err
	}
	now := a.store.now()
	out := make([]listRow, len(rows))
	for i, r := range rows {
		out[i] = listRow{Noun: "cmd", ID: "cmd/" + r.ID, Label: r.Name, Data: r.data(now)}
	}
	return out, nil
}

// addScript records a script with the author stamped from the environment
// (an explicit author name wins).
func (a *Service) addScript(in Add, author string) (Command, error) {
	in.Author = stampAuthor(os.Getenv, "owner")
	if author != "" {
		in.Author.Name = author
	}
	return a.store.Add(in)
}

func newJobID() (string, error) {
	id, err := token()
	if err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + id, nil
}

// captureRun runs a command off the terminal (stdin from the input or
// /dev/null), records it as job jobID and returns the recorded run. With
// jobs.keep 0 nothing is recorded and the output comes from memory.
func (a *Service) captureRun(ctx context.Context, row Command, args []string, assume bool, in *runInput, source, jobID string) (RunOutput, error) {
	defer in.close()
	row.JobID = jobID
	recorder := *a
	recorder.out, recorder.errOut, recorder.changed = io.Discard, io.Discard, nil
	cmd := withAssumeYes(scriptCommand(ctx, row, args), assume)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if in != nil {
		cmd.Stdin = in.file
	}
	finish, err := recorder.prepareRun(cmd, row, args, source, in)
	if err != nil {
		return RunOutput{}, err
	}
	var outBuf, errBuf bytes.Buffer
	if cmd.Stdout == nil {
		cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	}
	started := time.Now()
	runErr := a.run(cmd)
	if err := finish(runErr); err != nil {
		return RunOutput{}, err
	}
	if r, err := a.store.ReadRun(jobID); err == nil {
		return r, nil
	}
	ended := time.Now().UTC()
	r := RunOutput{Run: Run{ID: jobID, CommandID: row.ID, CommandName: row.Name, Args: args, Cwd: cmd.Dir, Started: started.UTC(), Ended: &ended, Duration: ended.Sub(started), Source: source, AssumeYes: assume}, Stdout: outBuf.String(), Stderr: errBuf.String()}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		r.ExitCode = &code
	}
	if runErr != nil && r.ExitCode == nil {
		r.Error = runErr.Error()
	}
	return r, nil
}

// runRecordedInput runs a command on the terminal, recording it while the
// script owns the stream; the script's exit status comes back as ExitError.
func (a *Service) runRecordedInput(ctx context.Context, row Command, argv []string, assume bool, input *runInput, source string) error {
	defer input.close()
	// A terminal sends these to the whole foreground process group. Let the
	// script receive them while the recorder survives to save its result.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGQUIT)
	defer signal.Stop(interrupts)
	cmd := withAssumeYes(scriptCommand(ctx, row, argv), assume)
	finish, e := a.prepareRun(cmd, row, argv, source, input)
	if e != nil {
		return e
	}
	cmd.Stdin = a.in
	if input != nil {
		cmd.Stdin = input.file
	}
	if cmd.Stdout == nil {
		cmd.Stdout, cmd.Stderr = a.out, a.errOut
	}
	runErr := a.run(cmd)
	return errors.Join(exitError(runErr), finish(runErr))
}

// runEdited opens the editor on an empty buffer and runs with it as stdin;
// an empty buffer cancels.
func (a *Service) runEdited(ctx context.Context, row Command, argv []string, assume bool) error {
	path, err := a.newInputBuffer()
	if err != nil {
		return err
	}
	defer os.Remove(path)
	editor := editorCommand(ctx, path)
	editor.Stdin, editor.Stdout, editor.Stderr = a.in, a.out, a.errOut
	if err := editedContent(path, a.run(editor)); err != nil {
		if errors.Is(err, errEmptyInput) {
			_, _ = fmt.Fprintln(a.errOut, err)
			return nil
		}
		return err
	}
	input, err := a.openInput(path, "edited")
	if err != nil {
		return err
	}
	return a.runRecordedInput(ctx, row, argv, assume, input, "cli")
}
