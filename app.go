package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/config"
	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/markdown"
	"ofam.dev/ochazuke/widgets/picker"
	"ofam.dev/ochazuke/widgets/table"
)

// Frame, node and view names.
const (
	pageID       = "main"
	nodeList     = "list"
	nodeFilter   = "filter"
	modalPalette = "palette"
	modalHelp    = "help"
	modalForm    = "form"
	modalViewer  = "viewer"

	viewCommands = "commands"
	viewJobs     = "jobs"

	kindCommand = "cmds.command"
	kindJob     = "cmds.job"
)

// list is one table view: its grid (rows, filter query, sort, cursor), the
// filter's Readline line and the chosen sort key.
type list struct {
	Grid table.Model
	Line input.Line
	Sort int // index into the view's sort keys; -1 = list order
}

// runningJob tracks a captured run started by this TUI session.
type runningJob struct{ CommandID, Name string }

// App is the page state, kept by value in the page frame's Data. Handlers
// get a private copy (clone) and the wrapper stores it back.
type App struct {
	View    string // viewCommands or viewJobs
	Cmds    []cmdRow
	Runs    []runRow
	JobsFor string // command ID the Jobs view is narrowed to; "" = every job
	Cmd     list
	Job     list
	Typing  bool // the filter line is open

	Detail    bool
	Ratio     float64
	DetailOff int
	detailKey string
	JobOut    *RunOutput // the focused job's captured output, read after each step
	jobOutKey string

	Running map[string]runningJob // job ID -> running job, for runs this session started
	Now     time.Time

	Status, Tone string
	Confirm      string
	prevConfirm  string
}

type sortKey struct{ key, title string }

var sortKeys = map[string][]sortKey{
	viewCommands: {{"num", "#"}, {"name", "Name"}, {"last_at", "Last"}, {"created", "Age"}, {"runs", "Runs"}, {"author", "Author"}},
	viewJobs:     {{"num", "#"}, {"command", "Command"}, {"started_at", "Started"}, {"exit_code", "Exit"}, {"duration_ns", "Duration"}, {"state", "State"}},
}

func (a App) clone() App {
	a.Running = maps.Clone(a.Running)
	a.Cmd.Grid, a.Job.Grid = a.Cmd.Grid.Copy(), a.Job.Grid.Copy()
	a.Cmd.Line.Text, a.Job.Line.Text = slices.Clone(a.Cmd.Line.Text), slices.Clone(a.Job.Line.Text)
	return a
}

// cur is the list of the shown view.
func (a *App) cur() *list {
	if a.View == viewJobs {
		return &a.Job
	}
	return &a.Cmd
}

func appOf(s core.State) App {
	a, _ := s.Pages[pageID].Data.(App)
	return a
}

func withApp(s core.State, a App) core.State {
	f := s.Pages[pageID]
	f.Data = a
	s.Pages = maps.Clone(s.Pages)
	s.Pages[pageID] = f
	return s
}

func (a *App) note(msg string) { a.Status, a.Tone = msg, "success" }
func (a *App) info(msg string) { a.Status, a.Tone = msg, "info" }
func (a *App) warn(msg string) { a.Status, a.Tone = msg, "warning" }
func (a *App) fail(msg string) { a.Status, a.Tone = msg, "error" }

// focusedCmd is the command under the cursor: the focused row, or in the
// Jobs view the focused job's command.
func (a *App) focusedCmd() (cmdRow, bool) {
	id := ""
	if a.View == viewJobs {
		r, ok := a.focusedJob()
		if !ok {
			return cmdRow{}, false
		}
		id = r.CommandID
	} else if row, ok := a.Cmd.Grid.FocusedRow(); ok {
		id = row.ID
	}
	for _, c := range a.Cmds {
		if c.ID == id && id != "" {
			return c, true
		}
	}
	return cmdRow{}, false
}

func (a *App) focusedJob() (runRow, bool) {
	row, ok := a.Job.Grid.FocusedRow()
	if !ok {
		return runRow{}, false
	}
	for _, r := range a.Runs {
		if r.ID == row.ID {
			return r, true
		}
	}
	return runRow{}, false
}

func focusRow(g *table.Model, id string) bool {
	for i, it := range g.Items() {
		if !it.Header && it.Row.ID == id {
			g.SelectedIdx = i
			g.Focused()
			return true
		}
	}
	return false
}

// load rereads the store and rebuilds both tables, keeping each cursor on
// the same record when it is still listed.
func (env *Env) load(a *App) {
	rows, err := env.Svc.rows("")
	if err != nil {
		a.fail("load commands: " + err.Error())
		return
	}
	runs, err := env.Svc.runRows("")
	if err != nil {
		a.fail("load jobs: " + err.Error())
		return
	}
	a.Cmds, a.Runs, a.Now = rows, runs, env.now()
	a.jobOutKey = "" // a running job's output may have grown
	env.rebuild(a)
}

func (env *Env) rebuild(a *App) {
	keepC, okC := a.Cmd.Grid.FocusedRow()
	keepJ, okJ := a.Job.Grid.FocusedRow()
	a.Cmd.Grid.Columns = cmdColumns(env.JobCols)
	a.Cmd.Grid.Rows = cmdTableRows(a.Cmds, a.Now)
	a.Job.Grid.Columns = jobColumns()
	a.Job.Grid.Rows = jobTableRows(a.Runs, a.JobsFor)
	if okC {
		focusRow(&a.Cmd.Grid, keepC.ID)
	}
	if okJ {
		focusRow(&a.Job.Grid, keepJ.ID)
	}
	a.Cmd.Grid.Focused()
	a.Job.Grid.Focused()
}

func cmdColumns(jobCols bool) []table.Column {
	cols := []table.Column{{Key: "num", Title: "#", Width: 4, Align: "right"}, {Key: "name", Title: "Name", Width: 24}, {Key: "desc", Title: "Description"}}
	if jobCols {
		cols = append(cols, table.Column{Key: "last", Title: "Last", Width: 5})
	}
	cols = append(cols, table.Column{Key: "age", Title: "Age", Width: 5})
	if jobCols {
		cols = append(cols, table.Column{Key: "runs", Title: "Runs", Width: 4, Align: "right"})
	}
	return append(cols, table.Column{Key: "author", Title: "Author", Width: 14})
}

func jobColumns() []table.Column {
	return []table.Column{
		{Key: "num", Title: "#", Width: 4, Align: "right"}, {Key: "command", Title: "Command", Width: 20},
		{Key: "started", Title: "Started", Width: 19}, {Key: "exit", Title: "Exit", Width: 6, Align: "right"},
		{Key: "duration", Title: "Duration", Width: 9, Align: "right"}, {Key: "state", Title: "State", Width: 8},
		{Key: "io", Title: "Out/Err"},
	}
}

func cmdTableRows(cmds []cmdRow, now time.Time) []table.Row {
	out := make([]table.Row, 0, len(cmds))
	for _, c := range cmds {
		num := "-"
		if c.Number > 0 {
			num = strconv.Itoa(c.Number)
		}
		name := c.Name
		if c.Pinned {
			name = "★ " + name
		}
		lastAt := ""
		if c.Status.Last != nil {
			lastAt = c.Status.Last.UTC().Format(time.RFC3339Nano)
		}
		cells := map[string]any{
			"num": num, "name": name, "desc": oneLine(c.Description), "last": lastCell(c.Status, now), "age": age(c.Created, now),
			"runs": c.Status.Count, "author": c.Author, "last_at": lastAt, "created": c.Created.UTC().Format(time.RFC3339Nano),
			"tags": strings.Join(c.Tags, " "), "status": c.Status.Status,
		}
		if c.Number > 0 {
			cells["num"] = c.Number
		}
		out = append(out, table.Row{ID: c.ID, Ref: &core.Ref{Kind: kindCommand, ID: c.ID}, Cells: cells, Unread: c.Status.Status == statusNew})
	}
	return out
}

func jobTableRows(runs []runRow, only string) []table.Row {
	out := make([]table.Row, 0, len(runs))
	for _, r := range runs {
		if only != "" && r.CommandID != only {
			continue
		}
		code := -1
		if r.ExitCode != nil {
			code = *r.ExitCode
		}
		name := r.CommandName
		if r.Script != nil {
			name = "line: " + oneLine(*r.Script)
		}
		dur := "-"
		if r.Ended != nil {
			dur = r.Duration.Round(time.Millisecond).String()
		}
		state := runState(r.Run)
		if r.AssumeYes {
			state += " Y"
		}
		cut := ""
		if r.StdoutCut || r.StderrCut {
			cut = " cut"
		}
		cells := map[string]any{
			"num": r.Number, "command": name, "started": r.Started.Local().Format("2006-01-02 15:04:05"), "exit": runExit(r.Run),
			"duration": dur, "state": state, "io": fmt.Sprintf("%d/%d B%s", r.StdoutBytes, r.StderrBytes, cut),
			"started_at": r.Started.UTC().Format(time.RFC3339Nano), "exit_code": code, "duration_ns": int64(r.Duration),
		}
		out = append(out, table.Row{ID: r.ID, Ref: &core.Ref{Kind: kindJob, ID: r.ID}, Cells: cells})
	}
	return out
}

// --- environment ------------------------------------------------------------

// Env is the session configuration shared by handlers and the view, plus
// the service that reads and writes the store.
type Env struct {
	Svc        *Service
	Km         *core.Keymap
	Bindings   []core.Binding
	Specs      []core.CommandSpec
	Theme      ui.Theme
	Copy       func(string) error // nil: yanks stay in the status line only
	Match      table.MatchMode
	Ratio      float64
	StackW     int
	Detail     bool
	JobCols    bool
	ShowOutput bool          // open the output viewer when a captured run ends
	Refresh    time.Duration // reload period; 0 = never
	StartNew   bool          // start on the newest command
	Notes      []string
	Now        func() time.Time
	Picked     *string // pick mode: the chosen script lands here

	jobs sync.Map // job ID -> chan struct{} closed when its run returns
}

func (env *Env) now() time.Time {
	if env.Now != nil {
		return env.Now()
	}
	return time.Now()
}

// Config is what main (or a test) supplies to build an Env.
type Config struct {
	Bindings []core.Binding   // user overrides, merged ahead of the defaults
	Theme    *ui.Theme        // nil: the default dark theme
	Settings *config.Settings // nil: defaults
	Copy     func(string) error
	Notes    []string
	Pick     bool // enter chooses a script (o-cmds pick) instead of running it
}

var modes = []core.Mode{{Name: "normal"}, {Name: "input"}, {Name: "palette"}, {Name: "help"}, {Name: "form"}, {Name: "viewer"}}

func newEnv(svc *Service, cfg Config) *Env {
	env := &Env{Svc: svc, Theme: ui.DefaultDarkTheme(), Copy: cfg.Copy, Notes: cfg.Notes}
	if cfg.Theme != nil {
		env.Theme = *cfg.Theme
	}
	set := cfg.Settings
	if set == nil {
		set, _ = config.LoadSettings(nil, defaultSettings)
	}
	switch m := table.MatchMode(set.String("matchMode", "substring")); m {
	case table.MatchSubstring, table.MatchFuzzy, table.MatchRegex:
		env.Match = m
	default:
		env.Match = table.MatchSubstring
		env.Notes = append(env.Notes, "settings: matchMode must be substring, fuzzy or regex")
	}
	env.Ratio = min(max(set.Float("detailRatio", 0.55), 0.2), 0.8)
	env.StackW = set.Int("stackWidth", 110)
	env.Detail = set.Bool("view.detail", true)
	env.JobCols = set.Bool("view.job-columns", true)
	env.ShowOutput = set.Bool("view.run-output", true)
	env.StartNew = set.Bool("list.start-newest", true)
	env.Refresh = time.Duration(max(set.Float("list.refresh", 2), 0) * float64(time.Second))
	env.Specs = commandSpecs()
	user := cfg.Bindings
	if cfg.Pick {
		// Pick mode is one more binding row: enter chooses instead of runs.
		user = append([]core.Binding{bs(viewCommands, "normal", "enter", "cmd.pick", "choose")}, user...)
	}
	for _, d := range config.CheckBindings(modes, cfg.Bindings, env.Specs...) {
		if d.Severity == config.SevError {
			env.Notes = append(env.Notes, fmt.Sprintf("bindings.json #%d: %s", d.Index, d.Message))
		}
	}
	env.Bindings = config.MergeBindings(defaultBindings(), user)
	km, err := core.NewKeymap(modes, env.Bindings, env.Specs...)
	if err != nil && len(env.Notes) == 0 {
		env.Notes = append(env.Notes, "bindings: "+strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	env.Km = km
	return env
}

// --- commands and bindings ------------------------------------------------------

func commandSpecs() []core.CommandSpec {
	user := func(id, desc string) core.CommandSpec {
		return core.CommandSpec{ID: id, Description: desc, Tags: []string{"user"}}
	}
	internal := func(id, desc string) core.CommandSpec { return core.CommandSpec{ID: id, Description: desc} }
	specs := []core.CommandSpec{
		user("cmd.run", "Run the focused command with its output captured as a job"),
		user("cmd.run.yes", "Run the focused command with OCHAZUKE_ASSUME_YES=1 (recorded; reruns repeat it)"),
		user("cmd.run.terminal", "Run the focused command on the terminal, suspending the UI"),
		user("cmd.rerun", "Repeat the focused command's latest job: its args, cwd, stdin and assume-yes"),
		user("cmd.add", "Record a new command in a form"),
		user("cmd.edit", "Edit the focused command's name, description, cwd and tags"),
		user("cmd.edit.script", "Open $VISUAL/$EDITOR on the focused command's script (undo restores it)"),
		user("cmd.delete", "Delete the focused command (its script and history stay; u undoes)"),
		user("cmd.undo", "Undo the most recent add, edit, delete, pin, dismiss or reset"),
		user("cmd.pin", "Pin or unpin the focused command"),
		user("cmd.dismiss", "Mark the focused command seen without running it"),
		user("cmd.undismiss", "Mark the focused command new again"),
		user("cmd.reset", "Forget the focused command's run count (press twice)"),
		user("cmd.view", "Read the focused command's script full screen"),
		user("cmd.yank", "Copy the focused command's script to the clipboard"),
		user("cmd.jobs", "Show the focused command's jobs"),
		user("cmd.pick", "Choose the focused command's script and quit (o-cmds pick)"),
		user("job.view", "Read the focused job's output full screen"),
		user("job.rerun", "Rerun the focused job with its args, cwd, stdin and assume-yes"),
		user("job.cancel", "Cancel the focused running job (in Commands: the command's newest running job)"),
		user("job.delete", "Delete the focused job and its captured streams"),
		user("view.toggle", "Switch between the Commands and Jobs tables"),
		user("filter.open", "Filter the shown table"),
		user("filter.mode.cycle", "Cycle the filter match mode: substring, fuzzy, regex"),
		user("filter.clear", "Clear the filter"),
		user("sort.cycle", "Sort the shown table by the next column (then list order)"),
		user("sort.reverse", "Reverse the sort direction"),
		user("order.toggle", "Number commands oldest-first or newest-first"),
		user("detail.toggle", "Show or hide the detail pane"),
		user("detail.scroll.down", "Scroll the detail pane down"),
		user("detail.scroll.up", "Scroll the detail pane up"),
		user("split.grow", "Widen the list next to the detail pane"),
		user("split.shrink", "Narrow the list next to the detail pane"),
		user("list.reload", "Reload commands and jobs from disk"),
		user("palette.open", "Open the command palette"),
		user("help.open", "Show the active key bindings"),
		user("app.close", "Quit"),

		internal("input.accept", "Keep the filter and return to the table"),
		internal("input.cancel", "Clear the filter and return to the table"),
		internal(input.CmdInsert, "Type into the filter"),
		internal("palette.insert", "Type into the palette query"),
		internal("palette.backspace", "Delete the last query character"),
		internal("palette.close", "Close the palette"),
		internal(picker.CmdConfirm, "Run the highlighted command"),
		internal(picker.CmdModeCycle, "Cycle the match mode"),
		internal("help.close", "Close help"),
		internal("viewer.close", "Close the viewer"),
		internal(form.CmdInsert, "Type into the field"),
		internal(form.CmdBackspace, "Delete the last character"),
		internal("form.clear", "Clear the field"),
		internal(form.CmdSubmit, "Save the form"),
		internal(form.CmdCancel, "Discard the form"),
		internal("run.done", "A job finished"),
		internal("run.cancelled", "A job was cancelled"),
		internal("edit.done", "The editor closed"),
		internal("yank.done", "Clipboard copy finished"),
		internal("data.poll", "Periodic reload"),
	}
	return append(specs, input.ReadlineSpecs()...)
}

func b(mode, keys, cmd, hint string) core.Binding {
	return core.Binding{Mode: mode, Keys: strings.Fields(keys), Command: cmd, Hint: hint}
}

// bs is a binding that fires only in one view (the page node's scope).
func bs(scope, mode, keys, cmd, hint string) core.Binding {
	bd := b(mode, keys, cmd, hint)
	bd.Scope = scope
	return bd
}

func class(mode, cls, cmd string, args map[string]any, hint string) core.Binding {
	return core.Binding{Mode: mode, Class: cls, Command: cmd, Args: args, Hint: hint}
}

// rowMotions are the standard motions a table uses.
var rowMotions = []string{"motion.down", "motion.up", "motion.top", "motion.bottom", "motion.page.down", "motion.page.up", "motion.half.down", "motion.half.up"}

func standard(mode string) []core.Binding {
	var out []core.Binding
	for _, bd := range core.StandardBindings(mode) {
		if slices.Contains(rowMotions, bd.Command) || bd.Command == core.CmdCountPush {
			out = append(out, bd)
		}
	}
	return out
}

func defaultBindings() []core.Binding {
	text := map[string]any{"text": "$text"}
	out := []core.Binding{
		bs(viewCommands, "normal", "enter", "cmd.run", "run"),
		bs(viewCommands, "normal", "g Y", "cmd.run.yes", "run, assume yes"),
		bs(viewCommands, "normal", "!", "cmd.run.terminal", "run on terminal"),
		bs(viewCommands, "normal", "g r", "cmd.rerun", "rerun last"),
		b("normal", "a", "cmd.add", "add"),
		bs(viewCommands, "normal", "e", "cmd.edit", "edit"),
		bs(viewCommands, "normal", "E", "cmd.edit.script", "edit script"),
		bs(viewCommands, "normal", "d d", "cmd.delete", "delete"),
		b("normal", "u", "cmd.undo", "undo"),
		bs(viewCommands, "normal", "p", "cmd.pin", "pin"),
		bs(viewCommands, "normal", "x", "cmd.dismiss", "dismiss"),
		bs(viewCommands, "normal", "X", "cmd.undismiss", "undismiss"),
		bs(viewCommands, "normal", "R", "cmd.reset", "reset runs"),
		b("normal", "v", "cmd.view", "view script"),
		b("normal", "y", "cmd.yank", "copy script"),
		bs(viewCommands, "normal", "J", "cmd.jobs", "jobs of command"),
		bs(viewJobs, "normal", "enter", "job.view", "view output"),
		bs(viewJobs, "normal", "g r", "job.rerun", "rerun"),
		bs(viewJobs, "normal", "r", "job.rerun", "rerun"),
		bs(viewJobs, "normal", "g Y", "cmd.run.yes", "run command, assume yes"),
		bs(viewJobs, "normal", "d d", "job.delete", "delete job"),
		b("normal", "C", "job.cancel", "cancel job"),
		b("normal", "tab", "view.toggle", "commands/jobs"),
		b("normal", "/", "filter.open", "filter"),
		b("normal", "alt+m", "filter.mode.cycle", "match mode"),
		b("normal", "esc", "filter.clear", "clear filter"),
		b("normal", "s", "sort.cycle", "sort"),
		b("normal", "S", "sort.reverse", "reverse sort"),
		b("normal", "o", "order.toggle", "number order"),
		b("normal", "w", "detail.toggle", "detail"),
		b("normal", "ctrl+w j", "detail.scroll.down", "scroll detail down"),
		b("normal", "ctrl+w k", "detail.scroll.up", "scroll detail up"),
		b("normal", "ctrl+w >", "split.grow", "wider list"),
		b("normal", "ctrl+w <", "split.shrink", "narrower list"),
		b("normal", "ctrl+l", "list.reload", "reload"),
		b("normal", ":", "palette.open", "commands"),
		b("normal", "?", "help.open", "help"),
		b("normal", "q", "app.close", "quit"),
		b("normal", "ctrl+c", "app.close", "quit"),
	}
	out = append(out, standard("normal")...)
	out = append(out,
		class("input", core.ClassPrintable, input.CmdInsert, text, "type"),
		class("input", core.ClassPaste, input.CmdInsert, text, "paste"),
		b("input", "enter", "input.accept", "keep filter"),
		b("input", "esc", "input.cancel", "clear filter"),
		b("input", "ctrl+c", "input.cancel", "clear filter"),
		b("input", "tab", "filter.mode.cycle", "match mode"),
		b("input", "alt+m", "filter.mode.cycle", "match mode"),
		b("input", "down", "motion.down", "next row"),
		b("input", "ctrl+n", "motion.down", "next row"),
		b("input", "up", "motion.up", "previous row"),
		b("input", "ctrl+p", "motion.up", "previous row"),

		class("palette", core.ClassPrintable, "palette.insert", text, "type"),
		class("palette", core.ClassPaste, "palette.insert", text, "paste"),
		b("palette", "backspace", "palette.backspace", "delete"),
		b("palette", "enter", picker.CmdConfirm, "run"),
		b("palette", "esc", "palette.close", "close"),
		b("palette", "ctrl+c", "palette.close", "close"),
		b("palette", "tab", picker.CmdModeCycle, "match mode"),
		b("palette", "alt+m", picker.CmdModeCycle, "match mode"),
		b("palette", "down", "motion.down", "next"),
		b("palette", "ctrl+n", "motion.down", "next"),
		b("palette", "up", "motion.up", "previous"),
		b("palette", "ctrl+p", "motion.up", "previous"),

		class("form", core.ClassPrintable, form.CmdInsert, text, "type"),
		class("form", core.ClassPaste, form.CmdInsert, text, "paste"),
		b("form", "backspace", form.CmdBackspace, "delete"),
		b("form", "tab", "motion.down", "next field"),
		b("form", "down", "motion.down", "next field"),
		b("form", "shift+tab", "motion.up", "previous field"),
		b("form", "up", "motion.up", "previous field"),
		b("form", "enter", form.CmdSubmit, "save"),
		b("form", "esc", form.CmdCancel, "cancel"),
		b("form", "ctrl+c", form.CmdCancel, "cancel"),

		b("viewer", "esc", "viewer.close", "close"),
		b("viewer", "q", "viewer.close", "close"),
		b("viewer", "enter", "viewer.close", "close"),
		b("viewer", "ctrl+c", "viewer.close", "close"),
		b("viewer", "space", "motion.page.down", "page down"),

		b("help", "esc", "help.close", "close"),
		b("help", "q", "help.close", "close"),
		b("help", "?", "help.close", "close"),
		b("help", "ctrl+c", "help.close", "close"),
	)
	// Readline editing in every text line; a mode's own rows win a clash.
	for _, mode := range []string{"input", "palette", "form"} {
		taken := map[string]bool{}
		for _, bd := range out {
			if bd.Mode == mode && len(bd.Keys) > 0 {
				taken[strings.Join(bd.Keys, " ")] = true
			}
		}
		for _, rb := range input.ReadlineBindings(mode, "") {
			if k := strings.Join(rb.Keys, " "); !taken[k] {
				taken[k] = true
				out = append(out, rb)
			}
		}
	}
	out = append(out, standard("viewer")...)
	return append(out, standard("help")...)
}

// --- engine -----------------------------------------------------------------------

type fn func(s core.State, a *App, c core.Command) (core.State, []core.Effect)

// wrap turns an app handler into a core.Handler: it copies the App, clears
// the status and any pending confirmation, sizes the tables, runs f, then
// refreshes what derives from the cursor and stores the App back.
func (env *Env) wrap(f fn) core.Handler {
	return func(s core.State, c core.Command) core.StepResult {
		a := appOf(s).clone()
		a.prevConfirm, a.Confirm = a.Confirm, ""
		a.Status, a.Tone = "", ""
		rows := env.geometry(&a, s.Width, s.Height).rows
		a.Cmd.Grid.PageSize, a.Job.Grid.PageSize = rows, rows
		s, effects := f(s, &a, c)
		a.prevConfirm = ""
		env.afterStep(&a)
		return core.Next(withApp(s, a), effects...)
	}
}

// afterStep resets the detail scroll when the focused record changes and
// reads the focused job's output for the detail pane.
func (env *Env) afterStep(a *App) {
	key := a.View + ":"
	if r, ok := a.cur().Grid.FocusedRow(); ok {
		key += r.ID
	}
	if key != a.detailKey {
		a.detailKey, a.DetailOff = key, 0
	}
	if a.View != viewJobs || !a.Detail {
		return
	}
	r, ok := a.focusedJob()
	if !ok {
		a.JobOut, a.jobOutKey = nil, ""
		return
	}
	if a.jobOutKey == r.ID {
		return
	}
	a.jobOutKey = r.ID
	if out, err := env.Svc.store.ReadRun(r.ID); err == nil {
		a.JobOut = &out
	} else {
		a.JobOut = &RunOutput{Run: r.Run, Stderr: "(output unavailable: " + err.Error() + ")"}
	}
}

func setPage(s core.State, mode, focus string) core.State {
	f := s.Pages[pageID]
	f.Mode, f.FocusID = mode, focus
	s.Pages = maps.Clone(s.Pages)
	s.Pages[pageID] = f
	return s
}

func openModal(s core.State, id, mode string, data any) core.State {
	s = closeModal(s, id)
	s.Modals = append(slices.Clone(s.Modals), core.Frame{ID: id, Kind: core.FrameModal, Mode: mode, FocusID: id, Data: data})
	return s
}

func closeModal(s core.State, id string) core.State {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		s.Modals = slices.Delete(slices.Clone(s.Modals), i, i+1)
	}
	return s
}

func modalData(s core.State, id string) any {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		return s.Modals[i].Data
	}
	return nil
}

func setModalData(s core.State, id string, data any) core.State {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		s.Modals = slices.Clone(s.Modals)
		s.Modals[i].Data = data
	}
	return s
}

// dispatch asks the shell to run cmd through the engine after this step
// (palette picks), so it meets the same handlers as a key.
func dispatch(c core.Command) core.Effect { return core.CustomEffect{Kind: "dispatch", Data: c} }

// viewerData is the full-screen reader's state.
type viewerData struct {
	Title, Source string
	Offset        int
}

// terminalRun asks the shell to run a command on the terminal.
type terminalRun struct {
	Row    Command
	Args   []string
	Assume bool
	JobID  string
}

// terminalDone is a terminal run's result.
type terminalDone struct{ ID, Name string }

func newEngine(env *Env) *core.Engine {
	e := core.NewEngine(env.Km)
	h := func(id string, f fn) { e.Handle(id, env.wrap(f)) }
	scoped := func(scope, id string, f fn) { e.HandleScope(scope, id, env.wrap(f)) }

	for _, id := range rowMotions {
		scoped(viewCommands, id, rowMotion)
		scoped(viewJobs, id, rowMotion)
		scoped("palette", id, env.paletteCmd)
		scoped("help", id, env.helpScroll)
		scoped("form", id, env.formCmd)
		scoped("viewer", id, viewerScroll)
	}

	h("cmd.run", env.runCaptured(false))
	h("cmd.run.yes", env.runCaptured(true))
	h("cmd.run.terminal", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		id, err := newJobID()
		if err != nil {
			a.fail(err.Error())
			return s, nil
		}
		return s, []core.Effect{core.CustomEffect{Kind: "run.terminal", Data: terminalRun{Row: row.Command, JobID: id}}}
	})
	h("cmd.rerun", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		r, err := env.Svc.latestRun(row.Command)
		if err != nil {
			a.warn(row.Name + ": " + err.Error())
			return s, nil
		}
		return s, env.rerun(a, r)
	})
	h("job.rerun", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		j, ok := a.focusedJob()
		if !ok {
			a.warn("no job under the cursor")
			return s, nil
		}
		r, err := env.Svc.store.ReadRun(j.ID)
		if err != nil {
			a.fail(err.Error())
			return s, nil
		}
		return s, env.rerun(a, r)
	})
	h("job.cancel", env.cancel)
	h("run.done", env.runDone)
	h("run.cancelled", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		id, _ := c.Args["value"].(string)
		name := a.Running[id].Name
		delete(a.Running, id)
		env.load(a)
		a.warn("cancelled " + name)
		return s, nil
	})

	h("cmd.add", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		return openModal(s, modalForm, "form", newCmdForm(nil)), nil
	})
	h("cmd.edit", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		return openModal(s, modalForm, "form", newCmdForm(&row.Command)), nil
	})
	for _, id := range append([]string{form.CmdInsert, form.CmdBackspace, "form.clear", form.CmdSubmit, form.CmdCancel}, input.ReadlineCommandIDs()...) {
		scoped("form", id, env.formCmd)
	}
	h("cmd.edit.script", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		// Checkpoint the current script so undo restores it.
		if _, err := env.Svc.store.Change("cmd/"+row.ID, func(*Command, []Command) {}); err != nil {
			a.fail(err.Error())
			return s, nil
		}
		ed := editorCommand(context.Background(), row.Path)
		return s, []core.Effect{core.ExecEffect{Name: ed.Args[0], Args: ed.Args[1:], Command: "edit.done"}}
	})
	h("edit.done", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		env.load(a)
		if err, _ := c.Args["error"].(error); err != nil {
			a.warn("editor: " + err.Error())
		} else {
			a.note("script saved · u undoes")
		}
		return s, nil
	})
	change := func(what string, f func(*Command, []Command)) fn {
		return func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
			row, ok := a.focusedCmd()
			if !ok {
				a.warn("no command under the cursor")
				return s, nil
			}
			if _, err := env.Svc.store.Change("cmd/"+row.ID, f); err != nil {
				a.fail(err.Error())
				return s, nil
			}
			env.load(a)
			a.note(what + " " + row.Name + " · u undoes")
			return s, nil
		}
	}
	h("cmd.delete", change("deleted", func(v *Command, _ []Command) { v.Deleted = true }))
	h("cmd.pin", change("pin toggled on", func(v *Command, _ []Command) { v.Pinned = !v.Pinned }))
	h("cmd.dismiss", change("dismissed", func(v *Command, _ []Command) { v.Dismissed = true }))
	h("cmd.undismiss", change("undismissed", func(v *Command, _ []Command) { v.Dismissed = false }))
	h("cmd.reset", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		if a.prevConfirm != "reset:"+row.ID {
			a.Confirm = "reset:" + row.ID
			a.warn(fmt.Sprintf("reset the run count of %s to 0? press R again (retained jobs stay; u undoes)", row.Name))
			return s, nil
		}
		var runs []Run
		for _, r := range a.Runs {
			runs = append(runs, r.Run)
		}
		at := env.Svc.store.now()
		return change("reset the run count of", func(v *Command, _ []Command) { resetCommand(v, runs, at) })(s, a, c)
	})
	h("cmd.undo", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		v, err := env.Svc.store.Undo()
		if err != nil {
			a.info(err.Error())
			return s, nil
		}
		env.load(a)
		focusRow(&a.Cmd.Grid, v.ID)
		a.note("undid the last change to " + v.Name)
		return s, nil
	})
	h("cmd.view", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		if a.View == viewJobs {
			return viewJob(env, s, a)
		}
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		return openModal(s, modalViewer, "viewer", viewerData{Title: "Script · " + row.Name, Source: cmdMarkdown(row, a.Now)}), nil
	})
	h("job.view", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) { return viewJob(env, s, a) })
	h("job.delete", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		j, ok := a.focusedJob()
		if !ok {
			a.warn("no job under the cursor")
			return s, nil
		}
		if err := env.Svc.store.DeleteRun(j.ID); err != nil {
			a.fail(err.Error())
			return s, nil
		}
		env.load(a)
		a.note(fmt.Sprintf("deleted job #%d", j.Number))
		return s, nil
	})
	h("cmd.yank", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		if env.Copy == nil {
			a.info("no clipboard (setting clipboard is off)")
			return s, nil
		}
		cp, text := env.Copy, row.Script
		a.info("copying " + row.Name)
		return s, []core.Effect{core.AsyncEffect{Slot: "clipboard", Command: "yank.done", Run: func(context.Context) (any, error) { return row.Name, cp(text) }}}
	})
	h("yank.done", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		if err, _ := c.Args["error"].(error); err != nil {
			a.warn("clipboard: " + err.Error())
		} else {
			a.note(fmt.Sprintf("copied the script of %v", c.Args["value"]))
		}
		return s, nil
	})
	h("cmd.pick", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok || env.Picked == nil {
			a.warn("no command under the cursor")
			return s, nil
		}
		*env.Picked = row.Script
		return s, []core.Effect{core.QuitEffect{}}
	})
	h("cmd.jobs", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		a.View, a.JobsFor = viewJobs, row.ID
		env.rebuild(a)
		a.Job.Grid.SelectedIdx = len(a.Job.Grid.Items()) - 1
		a.info("jobs of " + row.Name + " · tab shows every job")
		return s, nil
	})
	h("view.toggle", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		if a.View == viewJobs {
			a.View = viewCommands
		} else {
			a.View, a.JobsFor = viewJobs, ""
			env.rebuild(a)
		}
		return s, nil
	})

	h("filter.open", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		a.Typing = true
		return setPage(s, "input", nodeFilter), nil
	})
	h("filter.mode.cycle", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		l := a.cur()
		l.Grid.CycleMatchMode()
		l.Grid.Focused()
		a.info("match mode: " + string(l.Grid.MatchMode))
		return s, nil
	})
	h("filter.clear", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		l := a.cur()
		if l.Grid.Query == "" {
			if a.View == viewJobs && a.JobsFor != "" {
				a.JobsFor = ""
				env.rebuild(a)
				a.info("every job")
			}
			return s, nil
		}
		l.Grid.Query = ""
		l.Line.Set("")
		l.Grid.Focused()
		a.info("filter cleared")
		return s, nil
	})
	h(input.CmdInsert, func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		l := a.cur()
		l.Line.Sync(l.Grid.Query)
		if l.Line.Insert(c.Arg("text")) {
			l.Grid.Query = l.Line.Value()
			l.Grid.Focused()
		}
		return s, nil
	})
	for _, id := range input.ReadlineCommandIDs() {
		h(id, func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
			l := a.cur()
			l.Line.Sync(l.Grid.Query)
			if changed, _ := l.Line.HandleCommand(c); changed {
				l.Grid.Query = l.Line.Value()
				l.Grid.Focused()
			}
			return s, nil
		})
	}
	h("input.accept", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		a.Typing = false
		return setPage(s, "normal", nodeList), nil
	})
	h("input.cancel", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		a.Typing = false
		l := a.cur()
		l.Grid.Query = ""
		l.Line.Set("")
		l.Grid.Focused()
		return setPage(s, "normal", nodeList), nil
	})
	h("sort.cycle", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		l, keys := a.cur(), sortKeys[a.View]
		l.Sort++
		if l.Sort >= len(keys) {
			l.Sort = -1
		}
		applySort(l, keys)
		a.info(sortLabel(l, keys))
		return s, nil
	})
	h("sort.reverse", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		l, keys := a.cur(), sortKeys[a.View]
		if l.Sort < 0 {
			a.info("list order · s sorts")
			return s, nil
		}
		if l.Grid.SortDir == table.SortDesc {
			l.Grid.SortDir = table.SortAsc
		} else {
			l.Grid.SortDir = table.SortDesc
		}
		l.Grid.Focused()
		a.info(sortLabel(l, keys))
		return s, nil
	})
	h("order.toggle", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		if env.Svc.newestFirst() {
			env.Svc.order = "oldest-first"
		} else {
			env.Svc.order = "newest-first"
		}
		env.load(a)
		a.info("numbered " + env.Svc.order + " (this session; settings.json list.order keeps it)")
		return s, nil
	})
	h("detail.toggle", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		a.Detail = !a.Detail
		return s, nil
	})
	detailScroll := func(dir int) fn {
		return func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
			if !a.Detail {
				a.info("the detail pane is hidden; w shows it")
				return s, nil
			}
			m := markdown.New("", env.detailSource(a))
			m.Offset, m.PageSize = a.DetailOff, max(env.geometry(a, s.Width, s.Height).detailH-2, 1)
			id := "motion.half.down"
			if dir < 0 {
				id = "motion.half.up"
			}
			m.HandleCommand(core.Command{ID: id, Count: c.Count})
			a.DetailOff = m.Offset
			return s, nil
		}
	}
	h("detail.scroll.down", detailScroll(1))
	h("detail.scroll.up", detailScroll(-1))
	resize := func(d float64) fn {
		return func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
			a.Ratio = min(max(a.Ratio+d*float64(c.Times()), 0.2), 0.8)
			return s, nil
		}
	}
	h("split.grow", resize(0.05))
	h("split.shrink", resize(-0.05))
	h("list.reload", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		env.load(a)
		a.info(fmt.Sprintf("reloaded %d commands, %d jobs", len(a.Cmds), len(a.Runs)))
		return s, nil
	})
	h("data.poll", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		if len(s.Modals) == 0 {
			env.load(a)
		}
		return s, env.pollEffects()
	})

	h("palette.open", env.paletteOpen)
	for _, id := range append([]string{"palette.insert", "palette.backspace", "palette.close", picker.CmdConfirm, picker.CmdModeCycle}, input.ReadlineCommandIDs()...) {
		scoped("palette", id, env.paletteCmd)
	}
	h("help.open", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		return openModal(s, modalHelp, "help", 0), nil
	})
	h("help.close", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		return closeModal(s, modalHelp), nil
	})
	h("viewer.close", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		return closeModal(s, modalViewer), nil
	})
	h("app.close", func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		return s, []core.Effect{core.QuitEffect{}}
	})
	return e
}

func (env *Env) pollEffects() []core.Effect {
	if env.Refresh <= 0 {
		return nil
	}
	d := env.Refresh
	return []core.Effect{core.AsyncEffect{Slot: "poll", Command: "data.poll", Run: func(ctx context.Context) (any, error) {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
		return nil, nil
	}}}
}

func applySort(l *list, keys []sortKey) {
	if l.Sort < 0 || l.Sort >= len(keys) {
		l.Sort, l.Grid.SortField, l.Grid.SortDir = -1, "", table.SortNone
	} else {
		l.Grid.SortField, l.Grid.SortDir = keys[l.Sort].key, table.SortAsc
	}
	l.Grid.Focused()
}

func sortLabel(l *list, keys []sortKey) string {
	if l.Sort < 0 || l.Sort >= len(keys) || l.Grid.SortDir == table.SortNone {
		return "list order"
	}
	dir := "▲"
	if l.Grid.SortDir == table.SortDesc {
		dir = "▼"
	}
	return "sorted by " + keys[l.Sort].title + " " + dir
}

func rowMotion(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	g := &a.cur().Grid
	if (c.ID == "motion.bottom" || c.ID == "motion.top") && c.Count > 0 {
		g.SelectedIdx = c.Count - 1 // 5G: go to row 5
		g.Focused()
		return s, nil
	}
	g.HandleCommand(c)
	return s, nil
}

func viewJob(env *Env, s core.State, a *App) (core.State, []core.Effect) {
	j, ok := a.focusedJob()
	if !ok {
		a.warn("no job under the cursor")
		return s, nil
	}
	out, err := env.Svc.store.ReadRun(j.ID)
	if err != nil {
		a.fail(err.Error())
		return s, nil
	}
	return openModal(s, modalViewer, "viewer", viewerData{Title: fmt.Sprintf("Job #%d · %s", j.Number, out.CommandName), Source: jobMarkdown(out, j.Number)}), nil
}

func viewerScroll(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	v, ok := modalData(s, modalViewer).(viewerData)
	if !ok {
		return s, nil
	}
	m := markdown.New(v.Title, v.Source)
	m.Offset, m.PageSize = v.Offset, max(s.Height*9/10-4, 1)
	m.HandleCommand(c)
	v.Offset = m.Offset
	return setModalData(s, modalViewer, v), nil
}

// --- runs ---------------------------------------------------------------------------

// startRun starts a captured run of row off the UI loop: its job is
// recorded at once (so the Jobs table shows it running) and run.done
// reports it. Its slot lets job.cancel stop it.
func (env *Env) startRun(a *App, row Command, args []string, assume bool, in *runInput) []core.Effect {
	id, err := newJobID()
	if err != nil {
		in.close()
		a.fail(err.Error())
		return nil
	}
	done := make(chan struct{})
	env.jobs.Store(id, done)
	if a.Running == nil {
		a.Running = map[string]runningJob{}
	}
	a.Running[id] = runningJob{CommandID: row.ID, Name: row.Name}
	what := "running " + row.Name
	if assume {
		what += " (assume yes)"
	}
	a.info(what + " · C cancels")
	svc := env.Svc
	return []core.Effect{core.AsyncEffect{Slot: "run:" + id, Command: "run.done", Run: func(ctx context.Context) (any, error) {
		defer close(done)
		r, err := svc.captureRun(ctx, row, args, assume, in, "tui", id)
		r.ID = id
		if r.CommandName == "" {
			r.CommandName = row.Name
		}
		return r, err
	}}}
}

func (env *Env) runCaptured(assume bool) fn {
	return func(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
		row, ok := a.focusedCmd()
		if !ok {
			a.warn("no command under the cursor")
			return s, nil
		}
		return s, env.startRun(a, row.Command, nil, assume, nil)
	}
}

// rerun repeats a recorded run captured: its own line when it was unsaved,
// else the current script, with its args, cwd, stdin and assume-yes.
func (env *Env) rerun(a *App, r RunOutput) []core.Effect {
	row, err := env.Svc.rerunCommand(r)
	if err != nil {
		a.fail("rerun: " + err.Error())
		return nil
	}
	in, err := env.Svc.inputForRun(r.Run)
	if err != nil {
		a.fail("rerun: " + err.Error())
		return nil
	}
	return env.startRun(a, row, r.Args, r.AssumeYes, in)
}

func (env *Env) runDone(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	err, _ := c.Args["error"].(error)
	switch v := c.Args["value"].(type) {
	case RunOutput:
		delete(a.Running, v.ID)
		env.jobs.Delete(v.ID)
		env.load(a)
		if err != nil {
			a.fail(v.CommandName + ": " + err.Error())
			return s, nil
		}
		msg := fmt.Sprintf("%s: %s in %s", v.CommandName, runStatus(v.Run), v.Duration.Round(time.Millisecond))
		if v.ExitCode != nil && *v.ExitCode == 0 {
			a.note(msg)
		} else {
			a.fail(msg)
		}
		if env.ShowOutput {
			n := 0
			for _, r := range a.Runs {
				if r.ID == v.ID {
					n = r.Number
				}
			}
			s = openModal(s, modalViewer, "viewer", viewerData{Title: fmt.Sprintf("Job #%d · %s", n, v.CommandName), Source: jobMarkdown(v, n)})
		}
	case terminalDone:
		env.load(a)
		if err != nil {
			a.fail(v.Name + ": " + err.Error())
		} else if r, e := env.Svc.store.ReadRun(v.ID); e == nil {
			a.info(fmt.Sprintf("%s: %s in %s", v.Name, runStatus(r.Run), r.Duration.Round(time.Millisecond)))
		} else {
			a.info(v.Name + " finished")
		}
	default:
		env.load(a)
		if err != nil {
			a.fail(err.Error())
		}
	}
	return s, nil
}

// cancel stops a running captured job: a no-op effect in the job's slot
// cancels its context (the script is killed), then waits for its record.
func (env *Env) cancel(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	id := ""
	if a.View == viewJobs {
		if j, ok := a.focusedJob(); ok {
			id = j.ID
		}
	} else if row, ok := a.focusedCmd(); ok {
		for rid, rj := range a.Running {
			if rj.CommandID == row.ID && rid > id {
				id = rid
			}
		}
	}
	rj, ok := a.Running[id]
	if !ok || id == "" {
		a.warn("no running job here to cancel")
		return s, nil
	}
	ch, _ := env.jobs.Load(id)
	done, _ := ch.(chan struct{})
	a.info("cancelling " + rj.Name)
	return s, []core.Effect{core.AsyncEffect{Slot: "run:" + id, Command: "run.cancelled", Run: func(context.Context) (any, error) {
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
		return id, nil
	}}}
}

// terminal runs a command on the terminal with its output recorded, the
// UI suspended until the script exits.
func (env *Env) terminal(req terminalRun) tea.Cmd {
	svc := *env.Svc
	svc.out, svc.errOut, svc.changed = os.Stdout, os.Stderr, nil
	row := req.Row
	row.JobID = req.JobID
	cmd := withAssumeYes(scriptCommand(context.Background(), row, req.Args), req.Assume)
	finish, err := svc.prepareRun(cmd, row, req.Args, "tui", nil)
	if err != nil {
		return func() tea.Msg {
			return core.EffectResultEvent{Command: "run.done", Value: terminalDone{ID: req.JobID, Name: row.Name}, Err: err}
		}
	}
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		ferr := finish(runErr)
		return core.EffectResultEvent{Command: "run.done", Value: terminalDone{ID: req.JobID, Name: row.Name}, Err: ferr}
	})
}

// --- form --------------------------------------------------------------------------

// cmdForm is the add/edit modal: the form and the command it edits ("" adds).
type cmdForm struct {
	M  *form.Model
	ID string
}

func (f cmdForm) clone() cmdForm {
	m := *f.M
	m.Values = maps.Clone(m.Values)
	m.Errors = maps.Clone(m.Errors)
	f.M = &m
	return f
}

func newCmdForm(c *Command) cmdForm {
	specs := []core.ParamSpec{
		{Name: "name", Type: core.ParamString, Description: "unique; a taken name gets a suffix"},
		{Name: "description", Type: core.ParamString},
	}
	if c == nil {
		specs = append(specs, core.ParamSpec{Name: "script", Type: core.ParamString, Description: "one line; E opens $EDITOR for more"})
	}
	specs = append(specs, core.ParamSpec{Name: "cwd", Type: core.ParamString, Description: "working directory; empty runs where o-cmds runs"},
		core.ParamSpec{Name: "tags", Type: core.ParamList, Description: "comma separated"})
	init := map[string]any{}
	title, id := "Add a command", ""
	if c != nil {
		init = map[string]any{"name": c.Name, "description": c.Description, "cwd": c.Cwd, "tags": strings.Join(c.Tags, ", ")}
		title, id = "Edit "+c.Name, c.ID
	}
	m := form.New(title, specs, init)
	m.SubmitLabel = "Save (enter) · esc cancels"
	return cmdForm{M: m, ID: id}
}

func (env *Env) formCmd(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	old, ok := modalData(s, modalForm).(cmdForm)
	if !ok {
		return s, nil
	}
	f := old.clone()
	switch c.ID {
	case "form.clear":
		if i := f.M.ActiveIdx; i >= 0 && i < len(f.M.Specs) {
			f.M.SetValue(f.M.Specs[i].Name, "")
		}
	case form.CmdSubmit:
		res, _ := f.M.HandleCommand(c)
		if !res.Submitted {
			var msgs []string
			for _, name := range res.ErrorFields {
				msgs = append(msgs, name+": "+f.M.Errors[name])
			}
			a.fail("not saved — " + strings.Join(msgs, "; "))
			return setModalData(s, modalForm, f), nil
		}
		str := func(k string) string { v, _ := res.Values[k].(string); return strings.TrimSpace(v) }
		tags, _ := res.Values["tags"].([]string)
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
		tags = slices.DeleteFunc(tags, func(t string) bool { return t == "" })
		if f.ID == "" {
			script := str("script")
			if script != "" && !strings.HasSuffix(script, "\n") {
				script += "\n"
			}
			cmd, err := env.Svc.addScript(Add{Name: str("name"), Description: str("description"), Script: script, Cwd: str("cwd"), Tags: tags}, "")
			if err != nil {
				a.fail("add: " + err.Error())
				return setModalData(s, modalForm, f), nil
			}
			env.load(a)
			a.View = viewCommands
			focusRow(&a.Cmd.Grid, cmd.ID)
			a.note("added " + cmd.Name + " · E edits its script")
			return closeModal(s, modalForm), nil
		}
		cwd := str("cwd")
		if cwd != "" {
			if abs, err := filepath.Abs(cwd); err == nil {
				cwd = abs
			}
		}
		cmd, err := env.Svc.store.Change("cmd/"+f.ID, func(v *Command, rows []Command) {
			v.Name = uniqueName(str("name"), rows, v.ID)
			v.Description, v.Cwd, v.Tags = str("description"), cwd, tags
		})
		if err != nil {
			a.fail("edit: " + err.Error())
			return setModalData(s, modalForm, f), nil
		}
		env.load(a)
		a.note("saved " + cmd.Name + " · u undoes")
		return closeModal(s, modalForm), nil
	default:
		res, _ := f.M.HandleCommand(c)
		if res.Cancelled {
			a.info("form discarded")
			return closeModal(s, modalForm), nil
		}
	}
	return setModalData(s, modalForm, f), nil
}

// --- palette -------------------------------------------------------------------

func (env *Env) paletteOpen(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	keys := keysByCommand(env, "normal")
	var items []picker.Item
	for _, spec := range env.Specs {
		if spec.HasTag("user") {
			items = append(items, paletteItem(spec, keys))
		}
	}
	for _, spec := range core.MotionSpecs() {
		if slices.Contains(rowMotions, spec.ID) {
			items = append(items, paletteItem(spec, keys))
		}
	}
	m := picker.New("Commands", items)
	m.Scope, m.Placeholder, m.MatchMode = "palette", "type to filter commands · tab cycles match mode", picker.MatchMode(env.Match)
	return openModal(s, modalPalette, "palette", m), nil
}

func paletteItem(spec core.CommandSpec, keys map[string][]string) picker.Item {
	sub := spec.Description
	if k := keys[spec.ID]; len(k) > 0 {
		sub += "  [" + strings.Join(k, ", ") + "]"
	}
	return picker.Item{ID: spec.ID, Title: spec.ID, Subtitle: sub, Keywords: []string{spec.Description}}
}

func (env *Env) paletteCmd(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	old, ok := modalData(s, modalPalette).(*picker.Model)
	if !ok {
		return s, nil
	}
	m := *old
	switch c.ID {
	case "palette.insert":
		m.AppendQuery(c.Arg("text"))
	case "palette.backspace":
		m.BackspaceQuery()
	case "palette.close":
		return closeModal(s, modalPalette), nil
	default:
		m.PageSize = max(s.Height*70/100-4, 1)
		picked, _ := m.HandleCommand(c)
		if picked != nil {
			s = closeModal(s, modalPalette)
			return s, []core.Effect{dispatch(core.Command{ID: picked.ID, Source: "palette", Actor: core.Actor{Kind: "user"}})}
		}
	}
	if m.FilterErr != "" {
		a.warn("regex: " + m.FilterErr)
	}
	return setModalData(s, modalPalette, &m), nil
}

func (env *Env) helpScroll(s core.State, a *App, c core.Command) (core.State, []core.Effect) {
	off, _ := modalData(s, modalHelp).(int)
	total := len(env.helpLines())
	page := max(s.Height*85/100-2, 1)
	n := c.Times()
	switch c.ID {
	case "motion.down":
		off += n
	case "motion.up":
		off -= n
	case "motion.top":
		off = 0
	case "motion.bottom":
		off = total
	case "motion.page.down":
		off += n * page
	case "motion.page.up":
		off -= n * page
	case "motion.half.down":
		off += n * page / 2
	case "motion.half.up":
		off -= n * page / 2
	}
	off = min(max(off, 0), max(total-page, 0))
	return setModalData(s, modalHelp, off), nil
}

// --- shell ---------------------------------------------------------------------------

// newShell loads the store and builds the engine and tea shell.
func newShell(env *Env) *otea.Shell {
	a := App{View: viewCommands, Detail: env.Detail, Ratio: env.Ratio, Running: map[string]runningJob{}}
	a.Cmd.Grid, a.Job.Grid = *table.New(nil, nil), *table.New(nil, nil)
	a.Cmd.Grid.MatchMode, a.Job.Grid.MatchMode = env.Match, env.Match
	a.Cmd.Sort, a.Job.Sort = -1, -1
	env.load(&a)
	if env.StartNew && !env.Svc.newestFirst() {
		a.Cmd.Grid.SelectedIdx = len(a.Cmd.Grid.Rows) - 1
	}
	a.Job.Grid.SelectedIdx = len(a.Job.Grid.Rows) - 1
	a.Cmd.Grid.Focused()
	a.Job.Grid.Focused()
	if len(env.Notes) > 0 {
		a.warn(strings.Join(env.Notes, " · "))
	}
	initial := core.NewState(core.Frame{ID: pageID, FocusID: nodeList, Mode: "normal", Data: a})
	sh := otea.New(newEngine(env), initial, env.view, env.Theme)
	sh.OnPassThrough = func(e core.PassToWidgetEffect) tea.Cmd {
		id := map[string]string{"wheeldown": "motion.down", "wheelup": "motion.up"}[mouseButton(e)]
		if id == "" || e.NodeID != nodeList {
			return nil
		}
		return func() tea.Msg { return core.CommandEvent{Command: core.Command{ID: id, Count: 3, Source: "mouse"}} }
	}
	sh.OnCustom = func(e core.CustomEffect) tea.Cmd {
		switch d := e.Data.(type) {
		case core.Command:
			if e.Kind == "dispatch" {
				return func() tea.Msg { return core.CommandEvent{Command: d} }
			}
		case terminalRun:
			return env.terminal(d)
		}
		return nil
	}
	return sh
}

func mouseButton(e core.PassToWidgetEffect) string {
	if e.Mouse == nil {
		return ""
	}
	return e.Mouse.Button
}

// program starts the periodic reload with the shell.
type program struct {
	*otea.Shell
	env *Env
}

func (p program) Init() tea.Cmd {
	if p.env.Refresh <= 0 {
		return nil
	}
	return func() tea.Msg { return core.CommandEvent{Command: core.Command{ID: "data.poll", Source: "startup"}} }
}

func (p program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := p.Shell.Update(msg)
	return p, cmd
}

var errNoPick = errors.New("nothing chosen")
