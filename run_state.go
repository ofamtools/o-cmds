package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"ofam.dev/ochazuke"
)

// A command's run state: how often it ran since its last reset and whether
// the owner has seen it. It derives from the durable counter on the command
// (Command.RunsTotal), which runs.keep pruning never lowers, falling back to
// the retained run records for metadata written before the counter.
const (
	statusNew       = "new"       // never run and not dismissed
	statusRan       = "ran"       // run at least once since the last reset
	statusDismissed = "dismissed" // never run, marked seen (cmd.dismiss)
)

type runState struct {
	Count  int
	Last   *time.Time
	Status string
}

func runStateOf(c Command, runs []Run) runState {
	total, last := 0, c.LastRunAt
	if c.RunsTotal != nil {
		total = *c.RunsTotal
	} else {
		for _, r := range runs {
			if r.CommandID != c.ID {
				continue
			}
			total++
			if last == nil || r.Started.After(*last) {
				at := r.Started
				last = &at
			}
		}
	}
	st := runState{Count: max(0, total-c.RunsBase), Last: last}
	if st.Count == 0 || (c.ResetAt != nil && last != nil && !last.After(*c.ResetAt)) {
		st.Last = nil
	}
	switch {
	case st.Count > 0:
		st.Status = statusRan
	case c.Dismissed:
		st.Status = statusDismissed
	default:
		st.Status = statusNew
	}
	return st
}

// subjects are the commands a verb acts on: every row of a visual
// selection (Ctx.Subjects), else the one target.
func (a *App) subjects(c ochazuke.Ctx) ([]Command, error) {
	var out []Command
	seen := map[string]bool{}
	for _, inst := range c.Subjects {
		// A named target wins; only command rows are subjects (a CLI
		// request's context instance is the app, not a row).
		if inst == nil || ochazuke.StringArg(c.Args, "target", "") != "" {
			continue
		}
		t, _ := inst.Data.(string)
		if !strings.HasPrefix(t, "cmd/") {
			continue
		}
		row, err := a.store.Resolve(t, a.newestFirst())
		if err != nil {
			return nil, err
		}
		if !seen[row.ID] {
			seen[row.ID] = true
			out = append(out, row)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	row, err := a.target(c)
	if err != nil {
		return nil, err
	}
	return []Command{row}, nil
}

// changeEach applies one change to every subject as a single undo step.
func (a *App) changeEach(fn func(*Command)) ochazuke.AppHandler {
	return func(c ochazuke.Ctx) ochazuke.Result {
		rows, err := a.subjects(c)
		if err != nil {
			return ochazuke.Failed(err)
		}
		ids := make([]string, len(rows))
		for i, row := range rows {
			ids[i] = "cmd/" + row.ID
		}
		changed, err := a.store.ChangeAll(ids, func(v *Command, _ []Command) { fn(v) })
		a.refresh()
		if err != nil {
			return ochazuke.Failed(err)
		}
		if len(changed) == 1 {
			return ochazuke.Done(changed[0])
		}
		return ochazuke.Done(changed)
	}
}

func (a *App) dismiss() ochazuke.AppHandler {
	return a.changeEach(func(v *Command) { v.Dismissed = true })
}

func (a *App) undismiss() ochazuke.AppHandler {
	return a.changeEach(func(v *Command) { v.Dismissed = false })
}

// reset forgets a command's runs: its count goes to 0 and it reads as new
// again. Retained run records stay (cmd.runs-clear deletes them), so a
// reset loses nothing undo cannot bring back.
func (a *App) reset() ochazuke.AppHandler {
	return func(c ochazuke.Ctx) ochazuke.Result {
		runs, err := a.store.Runs("")
		if err != nil {
			return ochazuke.Failed(err)
		}
		at := a.store.now()
		return a.changeEach(func(v *Command) {
			if v.RunsTotal == nil {
				n := retainedRuns(runs, v.ID)
				v.RunsTotal = &n
			}
			v.RunsBase, v.ResetAt, v.Dismissed = *v.RunsTotal, &at, false
		})(c)
	}
}

// newTone is the tone a new command's cells draw in; "" turns it off.
func (a *App) newTone() string { return strings.TrimSpace(a.r.Settings().String("view.new-tone")) }

func validTone(v any) error {
	if s, _ := v.(string); strings.ContainsAny(strings.TrimSpace(s), " \t\n") {
		return errors.New("a tone name is one word")
	}
	return nil
}

func runCells(st runState, now time.Time) []string {
	last := "-"
	if st.Last != nil {
		last = age(*st.Last, now)
	}
	return []string{strconv.Itoa(st.Count), last}
}

func toneRow(tone string, cells int) []ochazuke.ToneSpan {
	if tone == "" {
		return nil
	}
	spans := make([]ochazuke.ToneSpan, cells)
	for i := range spans {
		spans[i] = ochazuke.CellTone(i, tone)
	}
	return spans
}
