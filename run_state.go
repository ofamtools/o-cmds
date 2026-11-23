package main

import (
	"fmt"
	"time"
)

// A command's run state: how often it ran since its last reset and whether
// the owner has seen it. It derives from the durable counter on the command
// (Command.RunsTotal), which jobs.keep pruning never lowers, falling back to
// the retained run records for metadata written before the counter.
const (
	statusNew       = "new"       // never run and not dismissed
	statusRan       = "ran"       // run at least once since the last reset
	statusDismissed = "dismissed" // never run, marked seen (dismiss)
)

type commandStatus struct {
	Count  int
	Last   *time.Time
	Status string
}

func commandStatusOf(c Command, runs []Run) commandStatus {
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
	st := commandStatus{Count: max(0, total-c.RunsBase), Last: last}
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

// resetCommand forgets a command's runs: its count goes to 0 and it reads
// as new again. Retained run records stay, so undo brings the count back.
func resetCommand(v *Command, runs []Run, at time.Time) {
	if v.RunsTotal == nil {
		n := retainedRuns(runs, v.ID)
		v.RunsTotal = &n
	}
	v.RunsBase, v.ResetAt, v.Dismissed = *v.RunsTotal, &at, false
}

func lastCell(st commandStatus, now time.Time) string {
	if st.Last == nil {
		return "-"
	}
	return age(*st.Last, now)
}

func age(at, now time.Time) string {
	d := now.Sub(at)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
