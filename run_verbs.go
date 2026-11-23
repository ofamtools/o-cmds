package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

type runRow struct {
	Run
	Number int `json:"number"`
}

// runRows numbers every retained run globally (oldest first) and keeps
// those of commandID ("" keeps all).
func (a *Service) runRows(commandID string) ([]runRow, error) {
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

// rerunCommand is what a rerun executes: the run's own line when it was
// unsaved, else the current saved script, in the run's cwd.
func (a *Service) rerunCommand(r RunOutput) (Command, error) {
	if r.Script != nil {
		return Command{ID: r.CommandID, Name: r.CommandName, Cwd: r.Cwd, Line: r.Script}, nil
	}
	c, e := a.store.Resolve(r.CommandID, false)
	c.Cwd = r.Cwd
	return c, e
}

// latestRun is the newest retained run of a command.
func (a *Service) latestRun(c Command) (RunOutput, error) {
	rows, err := a.store.Runs(c.ID)
	if err != nil {
		return RunOutput{}, err
	}
	if len(rows) == 0 {
		return RunOutput{}, errors.New("command has no recorded runs")
	}
	return a.store.ReadRun(rows[len(rows)-1].ID)
}

// rerunRecorded repeats a run on the terminal with its args, cwd, stdin and
// assume-yes, streaming and preserving the script's status.
func (a *Service) rerunRecorded(ctx context.Context, r RunOutput) error {
	c, err := a.rerunCommand(r)
	if err != nil {
		return err
	}
	in, err := a.inputForRun(r.Run)
	if err != nil {
		return err
	}
	return a.runRecordedInput(ctx, c, r.Args, r.AssumeYes, in, "cli")
}

func runStatus(r Run) string {
	if r.Ended == nil {
		return "running"
	}
	if r.Signal != "" {
		return "signal " + r.Signal
	}
	if r.ExitCode != nil {
		return "exit " + strconv.Itoa(*r.ExitCode)
	}
	return "error"
}

// runState is the one-word state the Jobs table shows.
func runState(r Run) string {
	switch {
	case r.Ended == nil:
		return "running"
	case r.Signal != "":
		return "killed"
	case r.ExitCode == nil:
		return "error"
	case *r.ExitCode == 0:
		return "ok"
	default:
		return "failed"
	}
}

// runExit is the Exit cell: the code, the signal, or "-".
func runExit(r Run) string {
	switch {
	case r.Signal != "":
		return r.Signal
	case r.ExitCode != nil:
		return strconv.Itoa(*r.ExitCode)
	}
	return "-"
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

// writeRunTable prints the jobs table the CLI has always printed.
func writeRunTable(out io.Writer, rows []runRow) error {
	var text strings.Builder
	fmt.Fprintln(&text, "#  Started              Exit           Duration       stdout/stderr bytes (kept)  Command  Run")
	color := false
	if f, ok := out.(*os.File); ok && term.IsTerminal(f.Fd()) {
		color = true
	}
	for _, row := range rows {
		r := row.Run
		status := runStatus(r)
		if color {
			c := "31"
			if r.ExitCode != nil && *r.ExitCode == 0 {
				c = "32"
			}
			status = "\x1b[" + c + "m" + status + "\x1b[0m"
		}
		cut := ""
		if r.StdoutCut || r.StderrCut {
			cut = " truncated"
		}
		fmt.Fprintf(&text, "%d  %s  %-14s %-14s %d/%d (%d/%d)%s  %s  %s\n", row.Number, r.Started.Local().Format("2006-01-02 15:04:05"), status, r.Duration.Round(time.Millisecond), r.StdoutBytes, r.StderrBytes, r.StdoutKept, r.StderrKept, cut, r.CommandName, r.ID)
	}
	_, e := io.WriteString(out, text.String())
	return e
}
