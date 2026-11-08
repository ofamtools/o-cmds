package main

import (
	"os/exec"
	"testing"
	"time"

	"ofam.dev/ochazuke"
)

func seedRun(t *testing.T, s *Store, c Command, id string, at time.Time) {
	t.Helper()
	code := 0
	r := Run{ID: id, CommandID: c.ID, CommandName: c.Name, Started: at, Ended: &at, ExitCode: &code}
	if e := s.locked(func() error { return s.writeRun(r, nil, nil) }); e != nil {
		t.Fatal(e)
	}
}

func TestRunStateDerivation(t *testing.T) {
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	later := at.Add(time.Hour)
	runs := []Run{{CommandID: "a", Started: at}, {CommandID: "a", Started: later}, {CommandID: "b", Started: at}}
	two, five := 2, 5
	for _, tc := range []struct {
		name   string
		c      Command
		count  int
		status string
		last   *time.Time
	}{
		{"never run", Command{ID: "c"}, 0, statusNew, nil},
		{"dismissed", Command{ID: "c", Dismissed: true}, 0, statusDismissed, nil},
		{"legacy counts retained runs", Command{ID: "a"}, 2, statusRan, &later},
		{"counter wins over retained runs", Command{ID: "b", RunsTotal: &five, LastRunAt: &later}, 5, statusRan, &later},
		{"reset is new again", Command{ID: "a", RunsTotal: &two, RunsBase: 2, ResetAt: &later, LastRunAt: &later}, 0, statusNew, nil},
		{"ran after a reset", Command{ID: "a", RunsTotal: &five, RunsBase: 2, ResetAt: &at, LastRunAt: &later}, 3, statusRan, &later},
		{"ran beats dismissed", Command{ID: "b", Dismissed: true}, 1, statusRan, &at},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := commandStatusOf(tc.c, runs)
			if st.Count != tc.count || st.Status != tc.status || (st.Last == nil) != (tc.last == nil) || (st.Last != nil && !st.Last.Equal(*tc.last)) {
				t.Fatalf("state=%+v", st)
			}
		})
	}
}

// The count is durable: jobs.keep 0 records no runs, yet each run counts,
// and a legacy command starts from its retained records.
func TestRunCountSurvivesKeepAndPruning(t *testing.T) {
	s := fixture(t)
	c := addCommand(t, s, "counted", "true\n")
	legacy := addCommand(t, s, "legacy", "true\n")
	seedRun(t, s, legacy, "old-1", s.now().Add(-time.Hour))
	a, _, _ := historyApp(t, s)
	a.run = func(cmd *exec.Cmd) error { return nil }
	if e := a.r.Settings().Set("jobs.keep", 0); e != nil {
		t.Fatal(e)
	}
	for _, row := range []Command{c, c, legacy} {
		finish, e := a.prepareRun(scriptCommand(background(), row, nil), row, nil, "cli")
		if e != nil {
			t.Fatal(e)
		}
		if e = finish(nil); e != nil {
			t.Fatal(e)
		}
	}
	runs, _ := s.Runs("")
	want := map[string]int{"counted": 2, "legacy": 2}
	rows, e := s.List()
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		st := commandStatusOf(row, runs)
		if st.Count != want[row.Name] || st.Status != statusRan || st.Last == nil {
			t.Fatalf("%s state=%+v", row.Name, st)
		}
	}
}

func TestDismissResetUndo(t *testing.T) {
	s := fixture(t)
	one := addCommand(t, s, "one", "true\n")
	addCommand(t, s, "two", "true\n")
	a, _, _ := historyApp(t, s)
	status := func(name string) commandStatus {
		t.Helper()
		c, e := s.Resolve(name, false)
		if e != nil {
			t.Fatal(e)
		}
		runs, _ := s.Runs("")
		return commandStatusOf(c, runs)
	}
	// A selection is one change: both dismissed, one undo restores both.
	call := func(verb string, args ochazuke.Arguments, subjects ...string) error {
		t.Helper()
		var insts []*ochazuke.Instance
		for _, name := range subjects {
			c, e := s.Resolve(name, false)
			if e != nil {
				t.Fatal(e)
			}
			insts = append(insts, &ochazuke.Instance{ID: c.ID, Data: "cmd/" + c.ID})
		}
		for _, spec := range a.verbs() {
			if spec.noun+"."+spec.name == verb {
				// Effects are checked through the store; a Result keeps its
				// error to itself.
				spec.handler(ochazuke.Ctx{Args: args, Subjects: insts})
				return nil
			}
		}
		t.Fatalf("no verb %s", verb)
		return nil
	}
	if e := call("cmd.dismiss", nil, "one", "two"); e != nil {
		t.Fatal(e)
	}
	if status("one").Status != statusDismissed || status("two").Status != statusDismissed {
		t.Fatal("selection not dismissed")
	}
	if _, e := s.Undo(); e != nil {
		t.Fatal(e)
	}
	if status("one").Status != statusNew || status("two").Status != statusNew {
		t.Fatal("undo did not restore the whole selection")
	}
	if e := call("cmd.dismiss", ochazuke.Arguments{"target": "two"}); e != nil {
		t.Fatal(e)
	}
	if e := call("cmd.undismiss", ochazuke.Arguments{"target": "two"}); e != nil || status("two").Status != statusNew {
		t.Fatalf("undismiss %v %+v", e, status("two"))
	}

	// Reset: count to 0 and new again; the retained records stay.
	for range 2 {
		if e := s.CountRun(one.ID, s.now()); e != nil {
			t.Fatal(e)
		}
	}
	seedRun(t, s, one, "kept-1", s.now())
	if e := call("cmd.dismiss", ochazuke.Arguments{"target": "one"}); e != nil {
		t.Fatal(e)
	}
	if e := call("cmd.reset", ochazuke.Arguments{"target": "one"}); e != nil {
		t.Fatal(e)
	}
	if st := status("one"); st.Count != 0 || st.Status != statusNew || st.Last != nil {
		t.Fatalf("after reset %+v", st)
	}
	if runs, _ := s.Runs(one.ID); len(runs) != 1 {
		t.Fatalf("reset removed run records: %v", runs)
	}
	// A run after the reset, then undo: the old count plus the new run.
	if e := s.CountRun(one.ID, s.now().Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	if st := status("one"); st.Count != 1 {
		t.Fatalf("after reset run %+v", st)
	}
	if _, e := s.Undo(); e != nil {
		t.Fatal(e)
	}
	if st := status("one"); st.Count != 3 || st.Status != statusRan {
		t.Fatalf("undo reset %+v", st)
	}
	// Undoing an earlier change never rolls the counter back.
	if _, e := s.Undo(); e != nil {
		t.Fatal(e)
	}
	if st := status("one"); st.Count != 3 {
		t.Fatalf("undo dismiss rolled back the count %+v", st)
	}
}

// Hosts (mtm) mount the published catalog page: the rows carry the run
// props and the new tone, so the colour reaches them with no o-cmds code.
func TestHostRowsCarryRunStateAndTone(t *testing.T) {
	s := fixture(t)
	fresh := addCommand(t, s, "fresh", "true\n")
	ran := addCommand(t, s, "ran", "true\n")
	gone := addCommand(t, s, "gone", "true\n")
	if e := s.CountRun(ran.ID, s.now()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ChangeAll([]string{"cmd/" + gone.ID}, func(c *Command, _ []Command) { c.Dismissed = true }); e != nil {
		t.Fatal(e)
	}
	_ = fresh
	for _, tc := range []struct {
		tone, want string
	}{{"", ochazuke.ToneQueued}, {"cmds.new", "cmds.new"}, {"off", ""}} {
		a, _, _ := historyApp(t, s)
		if tc.tone == "off" {
			if e := a.r.Settings().Set("view.new-tone", ""); e != nil {
				t.Fatal(e)
			}
		} else if tc.tone != "" {
			if e := a.r.Settings().Set("view.new-tone", tc.tone); e != nil {
				t.Fatal(e)
			}
		}
		p, e := mountHostPage(a)
		if e != nil {
			t.Fatal(e)
		}
		got := map[string]ochazuke.ListItem{}
		for _, item := range p.v.List(listID).Items() {
			got[item.Label] = item
		}
		for name, status := range map[string]string{"fresh": statusNew, "ran": statusRan, "gone": statusDismissed} {
			item := got[name]
			if item.Props["status"] != status {
				t.Fatalf("%s props=%v", name, item.Props)
			}
			toned := len(item.Tones) > 0
			if want := status == statusNew && tc.want != ""; toned != want || (toned && item.Tones[0].Tone != tc.want) {
				t.Fatalf("tone %q: %s tones=%v", tc.tone, name, item.Tones)
			}
			if len(item.Cells) != 7 {
				t.Fatalf("%s cells=%v", name, item.Cells)
			}
		}
		if n := got["ran"].Props["runs"]; n != float64(1) && n != 1 {
			t.Fatalf("runs prop=%#v", n)
		}
		// Runs is second from the right, before Author; Last follows Description.
		if got["ran"].Cells[5] != "1" || got["fresh"].Cells[3] != "-" || got["fresh"].Cells[6] != "owner" {
			t.Fatalf("run cells ran=%v fresh=%v", got["ran"].Cells, got["fresh"].Cells)
		}
	}
	// view.job-columns off drops the two columns and their cells.
	writeSettings(t, map[string]any{"view.job-columns": false})
	a, _, _ := historyApp(t, s)
	if cols := a.r.Nouns["cmd"].Columns; len(cols) != 5 {
		t.Fatalf("columns without run columns %v", cols)
	}
	rows, e := a.rows("")
	if e != nil || len(rows[0].Cells) != 5 {
		t.Fatalf("cells without run columns %v %v", rows, e)
	}
}

// Runs sits second from the right and outlasts the other optional columns,
// so it shows beside the detail pane at the default 120 columns.
func TestRunsColumnPlacement(t *testing.T) {
	cols := cmdColumns(true)
	if cols[len(cols)-2].Title != "Runs" || cols[len(cols)-1].Title != "Author" {
		t.Fatalf("columns=%v", cols)
	}
	for _, c := range cols {
		if c.Title != "Runs" && c.Priority != 0 && c.Priority <= cols[len(cols)-2].Priority {
			t.Fatalf("%s drops after Runs", c.Title)
		}
	}
}
