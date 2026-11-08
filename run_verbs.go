package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"ofam.dev/ochazuke"
)

var runColumns = []ochazuke.Column{
	{Title: "#", Width: 5}, {Title: "Started", Width: 20}, {Title: "Exit", Width: 12},
	{Title: "Duration", Width: 12}, {Title: "Out/Err", Width: 18},
}

func nonnegative(v any) error {
	if v.(int) < 0 {
		return fmt.Errorf("must be at least zero")
	}
	return nil
}

func (a *App) verbs() []verbSpec {
	specs := a.specs()
	for i := range specs {
		specs[i].noun = "cmd"
	}
	runs := a.runSpecs()
	for i := range runs {
		runs[i].noun = "job"
	}
	return append(specs, runs...)
}

func (a *App) runSpecs() []verbSpec {
	target := map[string]ochazuke.Parameter{"job": param("Job ID, unique prefix or global displayed number")}
	return []verbSpec{
		{name: "list", description: "List runs, newest at the bottom; optionally filter by command", params: map[string]ochazuke.Parameter{"command": param("Command name, number or ID")}, positional: []string{"command"}, handler: a.listRuns, output: "run-table"},
		{name: "show", description: "Read labelled run output in a text viewer", params: target, positional: []string{"job"}, handler: a.onRun(func(_ ochazuke.Ctx, r RunOutput) ochazuke.Result { return ochazuke.Done(runText(r)) }), text: true, output: "raw"},
		{name: "out", description: "Print captured stdout bytes", params: target, positional: []string{"job"}, handler: a.onRun(func(_ ochazuke.Ctx, r RunOutput) ochazuke.Result { return ochazuke.Done(r) }), output: "run-out"},
		{name: "in", description: "Print retained stdin bytes (may be truncated)", params: target, positional: []string{"job"}, handler: a.onRun(func(_ ochazuke.Ctx, r RunOutput) ochazuke.Result { return ochazuke.Done(r) }), output: "run-in"},
		{name: "err", description: "Print captured stderr bytes", params: target, positional: []string{"job"}, handler: a.onRun(func(_ ochazuke.Ctx, r RunOutput) ochazuke.Result { return ochazuke.Done(r) }), output: "run-err"},
		{name: "delete", description: "Delete one run and its captured streams", params: target, positional: []string{"job"}, handler: a.onRun(func(_ ochazuke.Ctx, r RunOutput) ochazuke.Result {
			e := a.store.DeleteRun(r.ID)
			a.refresh()
			return result(nil, e)
		}), destructive: true},
		{name: "rerun", foreground: true, description: "Execute the current saved script with the run's args, cwd and stdin", params: target, positional: []string{"job"}, handler: a.onRun(a.rerun), cli: a.rerunCLI},
	}
}

func (a *App) onRun(fn func(ochazuke.Ctx, RunOutput) ochazuke.Result) ochazuke.AppHandler {
	return func(c ochazuke.Ctx) ochazuke.Result {
		target := ochazuke.StringArg(c.Args, "job", "")
		if target == "" && c.Instance != nil {
			target, _ = c.Instance.Data.(string)
		}
		r, e := a.store.ReadRun(target)
		if e != nil {
			return ochazuke.Failed(e)
		}
		return fn(c, r)
	}
}

type runRow struct {
	Run
	Number int `json:"number"`
}

func (a *App) runRows(commandID string) ([]runRow, error) {
	runs, e := a.store.Runs("")
	if e != nil {
		return nil, e
	}
	rows := []runRow{}
	for i, r := range runs {
		if commandID == "" || r.CommandID == commandID {
			rows = append(rows, runRow{r, i + 1})
		}
	}
	return rows, nil
}

func (a *App) listRuns(c ochazuke.Ctx) ochazuke.Result {
	id := ""
	if target := ochazuke.StringArg(c.Args, "command", ""); target != "" {
		cmd, e := a.store.Resolve(target, a.newestFirst())
		if e != nil {
			return ochazuke.Failed(e)
		}
		id = cmd.ID
	}
	rows, e := a.runRows(id)
	return result(rows, e)
}

func (a *App) commandRuns(_ ochazuke.Ctx, c Command) ochazuke.Result {
	rows, e := a.runRows(c.ID)
	if e != nil {
		return ochazuke.Failed(e)
	}
	if a.tui {
		if !a.r.Settings().Bool("view.jobs") {
			return ochazuke.Done(nil)
		}
		choices := make([]ochazuke.PickerRow, 0, len(rows))
		for _, row := range rows {
			r := row.Run
			tone := ochazuke.ToneErrored
			if r.ExitCode != nil && *r.ExitCode == 0 {
				tone = ochazuke.ToneDone
			}
			choices = append(choices, ochazuke.PickerRow{ID: r.ID, Label: runSummary(r), Data: r.ID, Cells: []string{strconv.Itoa(row.Number), runSummary(r)}, Tones: []ochazuke.ToneSpan{{Cell: 1, Tone: tone}}})
		}
		_, e := a.r.OpenPicker(ochazuke.PickerSpec{Kind: "run-history", Stay: a.r.Settings().Bool("history.keep-open"), Title: "Runs: " + c.Name, Rows: choices, Plural: "runs", Empty: "no recorded runs", Preview: func(row ochazuke.PickerRow) string {
			r, e := a.store.ReadRun(row.ID)
			if e != nil {
				return e.Error()
			}
			return runText(r)
		}, Choose: func(row ochazuke.PickerRow) ochazuke.Result {
			r, e := a.store.ReadRun(row.ID)
			if e != nil {
				return ochazuke.Failed(e)
			}
			return a.showRun(ochazuke.Ctx{}, r)
		}})
		return result(nil, e)
	}
	return ochazuke.Done(rows)
}

func (a *App) clearRuns(_ ochazuke.Ctx, c Command) ochazuke.Result {
	e := a.store.ClearRuns(c.ID)
	a.refresh()
	return result(nil, e)
}

func (a *App) showRun(_ ochazuke.Ctx, r RunOutput) ochazuke.Result {
	if a.tui {
		return result(nil, openScriptViewer(a.r, "Run: "+r.CommandName, runText(r)))
	}
	return ochazuke.Done(r)
}

func (a *App) rerun(_ ochazuke.Ctx, r RunOutput) ochazuke.Result {
	c, e := a.rerunCommand(r)
	if e != nil {
		return ochazuke.Failed(e)
	}
	in, e := a.inputForRun(r.Run)
	if e != nil {
		return ochazuke.Failed(e)
	}
	return a.executeInput(c, r.Args, in)
}

func (a *App) rerunLast(c ochazuke.Ctx, row Command) ochazuke.Result {
	if a.tui && !a.r.Settings().Bool("view.jobs") {
		return ochazuke.Failed(errors.New("history UI disabled by view.jobs"))
	}
	rows, err := a.store.Runs(row.ID)
	if err != nil {
		return ochazuke.Failed(err)
	}
	if len(rows) == 0 {
		return ochazuke.Failed(errors.New("command has no recorded runs"))
	}
	r, err := a.store.ReadRun(rows[len(rows)-1].ID)
	if err != nil {
		return ochazuke.Failed(err)
	}
	return a.rerun(c, r)
}

func (a *App) rerunCommand(r RunOutput) (Command, error) {
	c, e := a.store.Resolve(r.CommandID, false)
	c.Cwd = r.Cwd
	return c, e
}

// Executing leaves use stream adapters, so framework JSON/error formatting
// cannot append to stdout or flatten the script's exit status.
func (a *App) executionTarget(name, field string, words []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	target := fs.String(field, "", "Name, ID, prefix or number")
	raw := fs.String("json", "{}", "Host arguments")
	fs.Bool("yes", false, "Host compatibility")
	var flags, positional []string
	for i := 0; i < len(words); i++ {
		word := words[i]
		if word == "--" {
			positional = append(positional, words[i+1:]...)
			break
		}
		if !strings.HasPrefix(word, "-") {
			positional = append(positional, word)
			continue
		}
		flags = append(flags, word)
		if !strings.Contains(word, "=") && word != "--yes" && word != "--help" && word != "-h" {
			i++
			if i == len(words) {
				return "", fmt.Errorf("%s needs a value", word)
			}
			flags = append(flags, words[i])
		}
	}
	if e := fs.Parse(flags); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return "", nil
		}
		return "", e
	}
	host := map[string]any{}
	if e := json.Unmarshal([]byte(*raw), &host); e != nil {
		return "", e
	}
	if *target == "" {
		*target, _ = host[field].(string)
	}
	if len(positional) > 1 {
		return "", fmt.Errorf("usage: %s TARGET", name)
	}
	if len(positional) == 1 {
		*target = positional[0]
	}
	if *target == "" {
		return "", errors.New("name a target or displayed number")
	}
	return *target, nil
}

func (a *App) rerunCLI(ctx context.Context, words []string) error {
	target, err := a.executionTarget("job rerun", "job", words)
	if err != nil || target == "" {
		return err
	}
	r, err := a.store.ReadRun(target)
	if err != nil {
		return err
	}
	return a.rerunRecorded(ctx, r)
}

func (a *App) rerunLastCLI(ctx context.Context, words []string) error {
	target, err := a.executionTarget("cmd rerun", "target", words)
	if err != nil || target == "" {
		return err
	}
	c, err := a.store.Resolve(target, a.newestFirst())
	if err != nil {
		return err
	}
	rows, err := a.store.Runs(c.ID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("command has no recorded runs")
	}
	r, err := a.store.ReadRun(rows[len(rows)-1].ID)
	if err != nil {
		return err
	}
	return a.rerunRecorded(ctx, r)
}

func (a *App) rerunRecorded(ctx context.Context, r RunOutput) error {
	c, err := a.rerunCommand(r)
	if err != nil {
		return err
	}
	in, err := a.inputForRun(r.Run)
	if err != nil {
		return err
	}
	return a.runRecordedInput(ctx, c, r.Args, in)
}

func runStatus(r Run) string {
	if r.Ended == nil {
		return "unfinished"
	}
	if r.Signal != "" {
		return "signal " + r.Signal
	}
	if r.ExitCode != nil {
		return "exit " + strconv.Itoa(*r.ExitCode)
	}
	return "error"
}

func runSummary(r Run) string {
	flags := ""
	if r.StdoutCut || r.StderrCut {
		flags += " truncated"
	}
	if r.CaptureError != "" {
		flags += " capture incomplete"
	}
	return fmt.Sprintf("%s  %s  %s  %d/%d B%s", r.Started.Local().Format("2006-01-02 15:04:05"), runStatus(r), r.Duration.Round(time.Millisecond), r.StdoutBytes, r.StderrBytes, flags)
}

func runText(r RunOutput) string {
	b, _ := json.MarshalIndent(r.Run, "", "  ")
	return runSummary(r.Run) + "\n\n--- stdin (retained) ---\n" + r.Input + "\n\n--- stdout ---\n" + r.Stdout + "\n--- stderr ---\n" + r.Stderr + "\n\n--- metadata ---\n" + string(b)
}

func (a *App) printRunResult(kind string, b []byte) error {
	if kind == "run-table" {
		var rows []runRow
		if e := json.Unmarshal(b, &rows); e != nil {
			return e
		}
		var text strings.Builder
		fmt.Fprintln(&text, "#  Started              Exit           Duration       stdout/stderr bytes (kept)  Command  Run")
		for _, row := range rows {
			r := row.Run
			status := runStatus(r)
			if f, ok := a.out.(*os.File); ok && term.IsTerminal(f.Fd()) {
				color := "31"
				if r.ExitCode != nil && *r.ExitCode == 0 {
					color = "32"
				}
				status = "\x1b[" + color + "m" + status + "\x1b[0m"
			}
			cut := ""
			if r.StdoutCut || r.StderrCut {
				cut = " truncated"
			}
			fmt.Fprintf(&text, "%d  %s  %-14s %-14s %d/%d (%d/%d)%s  %s  %s\n", row.Number, r.Started.Local().Format("2006-01-02 15:04:05"), status, r.Duration.Round(time.Millisecond), r.StdoutBytes, r.StderrBytes, r.StdoutKept, r.StderrKept, cut, r.CommandName, r.ID)
		}
		_, e := io.WriteString(a.out, text.String())
		return e
	}
	var r RunOutput
	if e := json.Unmarshal(b, &r); e != nil {
		return e
	}
	text := runText(r)
	encoded := ""
	switch kind {
	case "run-in":
		text = r.Input
		encoded = r.InputBase64
	case "run-out":
		text = r.Stdout
		encoded = r.StdoutBase64
	case "run-err":
		text = r.Stderr
		encoded = r.StderrBase64
	}
	if encoded != "" {
		raw, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			return e
		}
		_, e = a.out.Write(raw)
		return e
	}
	_, e := io.WriteString(a.out, text)
	return e
}
