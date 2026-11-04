package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

const version = "0.1.0"
const pageID = "cmds"
const listID = "cmds/commands"

type App struct {
	store       *Store
	r           *ochazuke.Runtime
	in          io.Reader
	out, errOut io.Writer
	run         Runner
	tui         bool
	changed     func()
	stdinNotice ochazuke.NoticeID
	// runCols is view.run-columns as read at start: the declared columns
	// and the rows' cells must agree for the whole process.
	runCols bool
}

type verbSpec struct {
	noun              string
	name, description string
	params            map[string]ochazuke.Parameter
	positional        []string
	handler           ochazuke.AppHandler
	rows              bool
	many              bool
	destructive       bool
	confirm, agent    string
	output            string
	text, foreground  bool
	slot              string
	timeout           time.Duration
	enabled           string
	cli               func(context.Context, []string) error
}

// cmdColumns are the command table's columns. runs adds Last (last-run
// age, dropped first at narrow widths) and Runs (the count since reset),
// second from the right before Author; Runs drops last of the optional
// columns, so it shows beside the detail pane at 120 columns.
func cmdColumns(runs bool) []ochazuke.Column {
	cols := []ochazuke.Column{
		{Title: "#", Width: 5, Align: ochazuke.AlignRight},
		{Title: "Name", Min: 12, Max: 28},
		{Title: "Description", Min: 12},
	}
	if runs {
		cols = append(cols, ochazuke.Column{Title: "Last", Width: 5, Priority: 4})
	}
	cols = append(cols, ochazuke.Column{Title: "Age", Width: 7, Priority: 3})
	if runs {
		cols = append(cols, ochazuke.Column{Title: "Runs", Width: 4, Align: ochazuke.AlignRight, Priority: 1})
	}
	return append(cols, ochazuke.Column{Title: "Author", Max: 24, Priority: 2})
}

// commandPage is the page the catalog publishes: its detail is built-in
// sections, which every host renders without an o-cmds detail kind.
func commandPage(detail bool) ocli.CatalogPage {
	p := ocli.CatalogPage{ID: pageID, Title: "Commands", Layout: ocli.LayoutSplit, Lists: []ocli.CatalogList{{ID: listID, Noun: "cmd", Source: "cmd.list", Title: "Commands"}}}
	if detail {
		p.Detail = &ocli.CatalogDetail{Fields: detailFields, Sections: detailSections}
	}
	return p
}

// standalonePage is the page o-cmds mounts itself, where the script detail
// kind is registered. Sections replace a kind, so view.detail-style=script
// drops them for the numbered script view; markdown keeps the hosts' look.
func standalonePage(r *ochazuke.Runtime) ocli.CatalogPage {
	p := commandPage(r.Settings().Bool("view.detail"))
	if p.Detail != nil && r.Settings().String("view.detail-style") == detailStyleScript {
		p.Detail = &ocli.CatalogDetail{Kind: scriptDetailKind, Fields: detailFields}
	}
	return p
}

func newApp(s *Store, in io.Reader, out, errOut io.Writer) (*App, error) {
	a := &App{store: s, in: in, out: out, errOut: errOut, run: func(c *exec.Cmd) error { return c.Run() }}
	a.r = ochazuke.NewRuntime()
	a.r.Config().App = "o-cmds"
	if err := a.r.DeclareSetting(
		ochazuke.Setting{Name: "runs.keep", Type: ochazuke.SettingInteger, Default: 20, Description: "Completed runs retained per command; 0 disables recording", Validate: nonnegative},
		ochazuke.Setting{Name: "runs.max-bytes", Type: ochazuke.SettingInteger, Default: 1024 * 1024, Description: "Bytes retained per stdout/stderr/stdin stream", Validate: nonnegative},
		ochazuke.Setting{Name: "view.stdin", Type: ochazuke.SettingBoolean, Default: true, Description: "Enable TUI stdin file picker and editor actions"},
		ochazuke.Setting{Name: "view.runs", Type: ochazuke.SettingBoolean, Default: true, Description: "Show last-run status and enable the TUI history view"},
		ochazuke.Setting{Name: "history.keep-open", Type: ochazuke.SettingBoolean, Default: true, Description: "Return from a run viewer to its history chooser; off closes history when a run is picked"},
		ochazuke.Setting{Name: "list.order", Type: ochazuke.SettingString, Default: "oldest-first", Enum: []string{"oldest-first", "newest-first"}, Description: "Command order, including numbered CLI targets"},
		ochazuke.Setting{Name: "list.start-newest", Type: ochazuke.SettingBoolean, Default: true, Description: "Start the standalone page on the newest command"},
		ochazuke.Setting{Name: "view.detail", Type: ochazuke.SettingBoolean, Default: true, Description: "Show the script and metadata beside the command list"},
		ochazuke.Setting{Name: "view.detail-style", Type: ochazuke.SettingString, Default: detailStyleScript, Enum: []string{detailStyleScript, detailStyleMarkdown}, Description: "Standalone detail: script (numbered lines) or markdown (the metadata and code block catalog hosts show)"},
		ochazuke.Setting{Name: "view.new-tone", Type: ochazuke.SettingString, Default: ochazuke.ToneQueued, Description: "Tone (a theme tone name) new commands, never run and not dismissed, draw in; empty turns the styling off", Validate: validTone},
		ochazuke.Setting{Name: "view.run-columns", Type: ochazuke.SettingBoolean, Default: true, Description: "Show the Runs (count since reset) and Last (last-run age) columns; read at start"},
		ochazuke.Setting{Name: "list.refresh", Type: ochazuke.SettingDuration, Default: 2 * time.Second, Description: "Reload the standalone list for agent additions; 0 disables polling"},
	); err != nil {
		return nil, err
	}
	for _, spec := range a.verbs() {
		b := a.r.Define(spec.noun).Verb(spec.name, func(c ochazuke.Ctx) ochazuke.Result {
			a.store.Actor = c.Actor()
			return spec.handler(c)
		}, spec.description)
		if len(spec.positional) > 0 {
			b.Positional(spec.positional...)
		}
		if spec.rows {
			b.Rows(spec.noun).Fetch().Quiet()
		}
		if spec.many {
			b.Many()
		}
		if spec.text {
			b.TextResult()
		}
		if spec.foreground {
			b.Foreground()
		}
		if spec.confirm != "" {
			b.Confirm(spec.confirm)
		}
		if spec.agent != "" {
			b.Agent(spec.agent)
		}
		if spec.destructive {
			b.Destructive()
		}
		op := a.r.Operations[spec.noun+"."+spec.name]
		op.Parameters = spec.params
		op.Slot, op.Timeout = spec.slot, spec.timeout
		op.EnabledSetting = spec.enabled
		a.r.Operations[op.Key()] = op
	}
	a.r.SetRecentPaths(a.recentInputs)
	a.r.DefineGuard("stdin-ui", func(r *ochazuke.Runtime, _ ochazuke.KeyEvent) bool { return r.Settings().Bool("view.stdin") })
	a.r.DefineGuard("history-ui", func(r *ochazuke.Runtime, _ ochazuke.KeyEvent) bool { return r.Settings().Bool("view.runs") })
	a.runCols = a.r.Settings().Bool("view.run-columns")
	a.r.Define("cmd").Columns(cmdColumns(a.runCols)...)
	a.r.Define("run").Columns(runColumns...)
	// Every application key is a binding row. Help/footer hints read the
	// runtime's effective bindings, including owner overrides.
	a.r.Define("cmd").Mode("normal").
		Bind("enter", ochazuke.Do("cmd.run-view")).
		Bind("g i", ochazuke.Do("cmd.run-file").Guard("stdin-ui")).
		Bind("g e", ochazuke.Do("cmd.edit-stdin").Guard("stdin-ui")).
		Bind("g r", ochazuke.Do("cmd.rerun").Guard("history-ui")).
		Bind("v", ochazuke.Do("cmd.view")).
		Bind("H", ochazuke.Do("cmd.history").Guard("history-ui")).
		Bind("y", ochazuke.Do("cmd.copy", ochazuke.Arguments{"yank": true})).
		Bind("e", ochazuke.Do("cmd.edit")).
		Bind("d d", ochazuke.Do("cmd.delete")).
		Bind("u", ochazuke.Do("cmd.undo")).
		Bind("s", ochazuke.Do("cmd.star")).
		Bind("x", ochazuke.Do("cmd.dismiss")).
		Bind("X", ochazuke.Do("cmd.undismiss")).
		Bind("R", ochazuke.Do("cmd.reset")).
		Bind("o", ochazuke.Do("ochazuke.setting.cycle", ochazuke.Arguments{"name": "list.order"})).
		Bind("ctrl+d", ochazuke.Do("ochazuke.detail.scroll", ochazuke.Arguments{"lines": 8}).Guard("detail-scrolls")).
		Bind("ctrl+u", ochazuke.Do("ochazuke.detail.scroll", ochazuke.Arguments{"lines": -8}).Guard("detail-scrolls"))
	registerScriptViewer(a.r)
	if err := a.declareSetup(); err != nil {
		return nil, err
	}
	if err := ocli.DeclarePage(a.r, commandPage(a.r.Settings().Bool("view.detail"))); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) shell() *ocli.Shell {
	sh := ocli.New(a.r, "o-cmds", a.in, a.out, a.errOut)
	sh.Version = version
	sh.Reveal = func(target string) error {
		row, err := a.store.Resolve(target, a.newestFirst())
		if err != nil {
			return err
		}
		list := a.r.List("cmd-targets")
		if list == nil {
			list, err = a.r.AddList("cmd-targets", nil)
			if err != nil {
				return err
			}
		}
		return list.SetItems([]ochazuke.ListItem{{ID: target, Noun: "cmd", Label: row.Name, Data: "cmd/" + row.ID}})
	}
	return sh
}

func param(desc string) ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Description: desc}
}

func (a *App) specs() []verbSpec {
	target := param("Command name, displayed number or stable ID (defaults to selected row)")
	return []verbSpec{
		{name: "view", description: "Read the command script in a text viewer", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.view), text: true},
		{name: "run-view", description: "Run and read captured stdout, stderr and exit status", params: map[string]ochazuke.Parameter{"target": target, "args": {Type: "array"}, "stdin": {Type: "path", Path: ochazuke.PathFile}}, positional: []string{"target"}, handler: a.on(a.runView), text: true, slot: "run", timeout: 15 * time.Minute},
		{name: "run-file", description: "Choose a stdin file and read the captured run", params: map[string]ochazuke.Parameter{"target": target, "stdin": {Type: "path", Path: ochazuke.PathFile, Required: true, Description: "File to use as stdin"}}, positional: []string{"target", "stdin"}, handler: a.on(a.runView), text: true, slot: "run", timeout: 15 * time.Minute, enabled: "view.stdin"},
		{name: "history", description: "Open full-screen command history on the terminal", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.history), foreground: true, cli: a.historyCLI, enabled: "view.runs"},
		{name: "stdin-file", description: "Choose a file to run with as stdin", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.stdinFile), enabled: "view.stdin"},
		{name: "edit-stdin", foreground: true, description: "Edit an empty buffer and run with it as stdin", params: map[string]ochazuke.Parameter{"target": target, "args": {Type: "array"}}, positional: []string{"target"}, handler: a.on(a.editStdin), enabled: "view.stdin", cli: func(ctx context.Context, args []string) error {
			return a.runCLI(ctx, append([]string{"--edit-stdin"}, args...))
		}},
		{name: "rerun", foreground: true, description: "Repeat the latest run with its original args, cwd and stdin", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.rerunLast), cli: a.rerunLastCLI, enabled: "view.runs"},
		{name: "runs", description: "List a command's run history, newest at the bottom", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.commandRuns), output: "run-table"},
		{name: "runs-clear", description: "Remove a command's captured run history", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.clearRuns), destructive: true},
		{name: "add", description: "Record a shell script, including an empty or unassociated one", params: map[string]ochazuke.Parameter{"name": param("Name; duplicates receive a suffix"), "desc": param("Description"), "file": param("Read script from file; otherwise script or stdin"), "script": param("Script text"), "cwd": param("Working directory; absence is allowed"), "tags": {Type: "array", Description: "Tags"}, "author": param("Author name; defaults to agent environment or owner")}, handler: a.add},
		{name: "list", description: "List numbered commands; newest at the bottom by default", params: map[string]ochazuke.Parameter{"filter": param("Match name, description or tags")}, handler: a.list, rows: true, output: "table"},
		{name: "show", description: "Show a command's script and metadata", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.show), output: "script"},
		{name: "run", foreground: true, description: "Run with sh in the recorded cwd, streaming output and preserving exit status", params: map[string]ochazuke.Parameter{"target": target, "args": {Type: "array", Description: "Script arguments (CLI also accepts -- args...)"}, "stdin": {Type: "path", Path: ochazuke.PathFile, Description: "File to use as stdin"}, "edit-stdin": {Type: "boolean", Description: "Edit an empty stdin buffer"}, "stdin-kind": {Type: "string", Enum: []string{"file", "edited"}, Description: "Input provenance; edited retains input for reruns"}}, positional: []string{"target"}, handler: a.on(a.execute), cli: a.runCLI},
		{name: "copy", description: "Print the script, or --yank it to the clipboard", params: map[string]ochazuke.Parameter{"target": target, "yank": {Type: "boolean", Description: "Copy to clipboard"}}, positional: []string{"target"}, handler: a.on(a.copy), output: "script"},
		{name: "edit", foreground: true, description: "Open $VISUAL/$EDITOR on the script; checkpoint its current contents for undo", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.edit)},
		{name: "rename", description: "Rename immediately; duplicates receive a suffix", params: map[string]ochazuke.Parameter{"target": target, "name": param("New name")}, positional: []string{"target", "name"}, handler: a.on(a.rename)},
		{name: "delete", description: "Hide a command immediately, retaining its script and undo history; agents use cmd.archive", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.archive(true)), agent: ochazuke.AgentNever},
		{name: "archive", description: "Hide a command, retaining its script and undo history", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.archive(true)), agent: ochazuke.AgentAllow},
		{name: "restore", description: "Restore an archived command by name or stable ID", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.archive(false)), agent: ochazuke.AgentAllow},
		{name: "undo", description: "Undo the most recent rename, delete, archive, restore, edit, pin, dismiss or reset change", handler: a.undo},
		{name: "dismiss", description: "Mark a command (or the selected rows) seen without running it, so it no longer reads as new", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.dismiss(), many: true, agent: ochazuke.AgentAllow},
		{name: "undismiss", description: "Mark a dismissed command (or the selected rows) new again", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.undismiss(), many: true, agent: ochazuke.AgentAllow},
		{name: "reset", description: "Forget a command's runs: count to 0, new again; retained run records stay (cmd runs-clear deletes them)", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.reset(), many: true, confirm: "Reset the run count of {{label}} to 0? Retained run records stay; undo restores the count.", agent: ochazuke.AgentAllow},
		{name: "pin", description: "Pin a command without moving its chronological position", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.pin(true))},
		{name: "unpin", description: "Unpin a command", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.pin(false))},
		{name: "star", description: "Toggle a command's pin", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.on(a.star)},
		{name: "path", description: "Print a command's script path, or the storage directory", params: map[string]ochazuke.Parameter{"target": target}, positional: []string{"target"}, handler: a.path, output: "path"},
	}
}

func (a *App) newestFirst() bool { return a.r.Settings().String("list.order") == "newest-first" }

func (a *App) target(c ochazuke.Ctx) (Command, error) {
	t := ochazuke.StringArg(c.Args, "target", "")
	if t == "" && c.Instance != nil {
		t, _ = c.Instance.Data.(string)
		if t == "" {
			t = c.Instance.ID
		}
	}
	if t == "" {
		return Command{}, fmt.Errorf("name a command or displayed number")
	}
	return a.store.Resolve(t, a.newestFirst())
}

func (a *App) on(fn func(ochazuke.Ctx, Command) ochazuke.Result) ochazuke.AppHandler {
	return func(c ochazuke.Ctx) ochazuke.Result {
		row, err := a.target(c)
		if err != nil {
			return ochazuke.Failed(err)
		}
		return fn(c, row)
	}
}

func result(v any, err error) ochazuke.Result {
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(v)
}
func (a *App) refresh() {
	if a.changed != nil {
		a.changed()
	}
}

func stringList(v any) []string {
	switch v := v.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case string:
		var out []string
		if json.Unmarshal([]byte(v), &out) == nil {
			return out
		}
		if v != "" {
			return []string{v}
		}
	}
	return nil
}

func (a *App) add(c ochazuke.Ctx) ochazuke.Result {
	script, hasScript := c.Args["script"].(string)
	if file := ochazuke.StringArg(c.Args, "file", ""); file != "" {
		b, e := os.ReadFile(file)
		if e != nil {
			return ochazuke.Failed(e)
		}
		script = string(b)
	} else if !hasScript && !a.tui {
		b, e := io.ReadAll(a.in)
		if e != nil {
			return ochazuke.Failed(e)
		}
		script = string(b)
	}
	author := stampAuthor(os.Getenv, "owner")
	if name := ochazuke.StringArg(c.Args, "author", ""); name != "" {
		author.Name = name
	}
	row, e := a.store.Add(Add{Name: ochazuke.StringArg(c.Args, "name", ""), Description: ochazuke.StringArg(c.Args, "desc", ""), Script: script, Cwd: ochazuke.StringArg(c.Args, "cwd", ""), Tags: stringList(c.Args["tags"]), Author: author})
	a.refresh()
	return result(row, e)
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

func (a *App) rows(filter string) ([]ocli.Row, error) {
	cs, e := a.store.records(!a.r.Settings().Bool("list.hide-archived"))
	if e != nil {
		return nil, e
	}
	if a.newestFirst() {
		slices.Reverse(cs)
	}
	out := []ocli.Row{}
	filter = strings.ToLower(filter)
	lastRuns := map[string]Run{}
	runs, err := a.store.Runs("")
	if err != nil {
		return nil, err
	}
	if a.r.Settings().Bool("view.runs") {
		for _, r := range runs {
			lastRuns[r.CommandID] = r
		}
	}
	tone, now := a.newTone(), a.store.now()
	number := 0
	for _, c := range cs {
		cellNumber := "-"
		if !c.Deleted {
			number++
			cellNumber = strconv.Itoa(number)
		}
		if !strings.Contains(strings.ToLower(c.Name+" "+c.Description+" "+strings.Join(c.Tags, " ")), filter) {
			continue
		}
		text, e := a.store.Script(c)
		if e != nil {
			return nil, e
		}
		// Match the object shape a catalog host obtains by decoding JSON.
		// PageView's field renderer consumes maps rather than Go structs.
		encoded, e := json.Marshal(c)
		if e != nil {
			return nil, e
		}
		var data map[string]any
		if e = json.Unmarshal(encoded, &data); e != nil {
			return nil, e
		}
		data["script"] = text
		data["detail_md"] = scriptMarkdown(text, c.Path)
		data["age"] = age(c.Created, now)
		if run, ok := lastRuns[c.ID]; ok {
			data["last_run"] = runSummary(run)
		}
		st := runStateOf(c, runs)
		data["runs"], data["status"] = st.Count, st.Status
		if st.Last != nil {
			data["last_run_at"] = st.Last.Format(time.RFC3339)
			data["last_run_age"] = age(*st.Last, now)
		}
		name := c.Name
		if c.Pinned {
			name = "★ " + name
		}
		author := c.Author.Name
		if c.Author.Harness != "" && c.Author.Harness != author {
			author += "/" + c.Author.Harness
		}
		if c.Author.Pane != "" {
			author += "@" + c.Author.Pane
		}
		data["author_label"] = author
		cells := []string{cellNumber, name, c.Description}
		if a.runCols {
			cells = append(cells, lastCell(st, now), age(c.Created, now), strconv.Itoa(st.Count), author)
		} else {
			cells = append(cells, age(c.Created, now), author)
		}
		row := ocli.Row{Noun: "cmd", ID: "cmd/" + c.ID, Label: c.Name, Data: data, Cells: cells}
		if st.Status == statusNew {
			row.Tones = toneRow(tone, len(cells))
		}
		out = append(out, row)
	}
	return out, nil
}

func (a *App) list(c ochazuke.Ctx) ochazuke.Result {
	rows, e := a.rows(ochazuke.StringArg(c.Args, "filter", ""))
	return result(rows, e)
}
func (a *App) show(c ochazuke.Ctx, row Command) ochazuke.Result {
	text, e := a.store.Script(row)
	if e != nil {
		return ochazuke.Failed(e)
	}
	if a.tui {
		return result(nil, openScriptViewer(a.r, row.Name, text))
	}
	return ochazuke.Done(struct {
		Command
		Script string `json:"script"`
	}{row, text})
}

func (a *App) execute(c ochazuke.Ctx, row Command) ochazuke.Result {
	path := ochazuke.StringArg(c.Args, "stdin", "")
	if edit, _ := c.Args["edit-stdin"].(bool); edit {
		if path != "" {
			return ochazuke.Failed(fmt.Errorf("--stdin and --edit-stdin are mutually exclusive"))
		}
		return a.editStdin(c, row)
	}
	in, err := a.openInput(path, ochazuke.StringArg(c.Args, "stdin-kind", "file"))
	if err != nil {
		return ochazuke.Failed(err)
	}
	return a.executeInput(row, stringList(c.Args["args"]), in)
}

func (a *App) executeInput(row Command, args []string, in *runInput) ochazuke.Result {
	a.clearInputNotice()
	if !a.tui {
		return result(nil, a.runRecordedInput(background(), row, args, in))
	}
	cmd := scriptCommand(background(), row, args)
	finish, err := a.prepareRun(cmd, row, args, "tui", in)
	if err != nil {
		in.close()
		return ochazuke.Failed(err)
	}
	if in != nil {
		cmd.Stdin = in.file
	}
	return ochazuke.Foreground(cmd).Then(func(c ochazuke.Ctx, _ any, runErr error) {
		defer in.close()
		if err := finish(runErr); err != nil {
			c.Error("save run: " + err.Error())
		}
		a.refresh()
	})
}

func (a *App) copy(c ochazuke.Ctx, row Command) ochazuke.Result {
	text, e := a.store.Script(row)
	if e != nil {
		return ochazuke.Failed(e)
	}
	if yank, _ := c.Args["yank"].(bool); yank {
		if e = clipboard(background(), text, a.run, a.errOut); e != nil {
			return ochazuke.Failed(e)
		}
	}
	return ochazuke.Done(map[string]string{"script": text})
}

func (a *App) edit(c ochazuke.Ctx, row Command) ochazuke.Result {
	_, e := a.store.Change(row.ID, func(*Command, []Command) {})
	if e != nil {
		return ochazuke.Failed(e)
	}
	cmd := editorCommand(background(), row.Path)
	if a.tui {
		return ochazuke.Foreground(cmd).Then(func(ochazuke.Ctx, any, error) { a.refresh() })
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.in, a.out, a.errOut
	e = a.run(cmd)
	a.refresh()
	return result(row, exitError(e))
}

func (a *App) change(row Command, fn func(*Command, []Command)) ochazuke.Result {
	v, e := a.store.Change(row.ID, fn)
	a.refresh()
	return result(v, e)
}
func (a *App) rename(c ochazuke.Ctx, row Command) ochazuke.Result {
	return a.change(row, func(v *Command, rows []Command) {
		v.Name = uniqueName(ochazuke.StringArg(c.Args, "name", ""), rows, v.ID)
	})
}
func (a *App) archive(value bool) func(ochazuke.Ctx, Command) ochazuke.Result {
	return func(_ ochazuke.Ctx, row Command) ochazuke.Result {
		return a.change(row, func(v *Command, rows []Command) {
			v.Deleted = value
			if !value {
				v.Name = uniqueName(v.Name, rows, v.ID)
			}
		})
	}
}
func (a *App) pin(value bool) func(ochazuke.Ctx, Command) ochazuke.Result {
	return func(_ ochazuke.Ctx, row Command) ochazuke.Result {
		return a.change(row, func(v *Command, _ []Command) { v.Pinned = value })
	}
}
func (a *App) star(_ ochazuke.Ctx, row Command) ochazuke.Result {
	return a.change(row, func(v *Command, _ []Command) { v.Pinned = !v.Pinned })
}
func (a *App) undo(ochazuke.Ctx) ochazuke.Result {
	v, e := a.store.Undo()
	a.refresh()
	return result(v, e)
}
func (a *App) path(c ochazuke.Ctx) ochazuke.Result {
	if ochazuke.StringArg(c.Args, "target", "") == "" && c.Instance == nil {
		return ochazuke.Done(a.store.Dir)
	}
	row, e := a.target(c)
	if e != nil {
		return ochazuke.Failed(e)
	}
	return ochazuke.Done(row.Path)
}

// JSON args used by catalog hosts share the same argument representation.
func background() context.Context { return context.Background() }
