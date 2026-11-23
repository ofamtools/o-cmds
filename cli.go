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
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/widgets/table"
)

// --- Matomeru extension catalog & query wire protocol -----------------------

// extSchema matches matomeru-next/data.Schema on the wire.
type extSchema struct {
	Kind           string           `json:"kind"`
	Title          string           `json:"title"`
	Plural         string           `json:"plural"`
	Fields         []core.ParamSpec `json:"fields"`
	DefaultColumns []string         `json:"defaultColumns,omitempty"`
	SummaryField   string           `json:"summaryField,omitempty"`
	UniqueFields   []string         `json:"uniqueFields,omitempty"`
}

// extManifest matches matomeru-next/ext.Manifest on the wire.
type extManifest struct {
	Name     string             `json:"name"`
	Version  string             `json:"version,omitempty"`
	Schemas  []extSchema        `json:"schemas,omitempty"`
	Commands []core.CommandSpec `json:"commands,omitempty"`
	Bindings []core.Binding     `json:"bindings,omitempty"`
}

// extRecord matches matomeru-next/data.Record on the wire.
type extRecord struct {
	Kind          string         `json:"kind"`
	ID            string         `json:"id"`
	Fields        map[string]any `json:"fields"`
	ArchiveStatus string         `json:"archiveStatus"`
	StarStatus    string         `json:"starStatus"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// extQueryResult matches matomeru-next/data.QueryResult on the wire.
type extQueryResult struct {
	Items     []extRecord `json:"items"`
	Total     int         `json:"total"`
	FilterErr string      `json:"filterErr,omitempty"`
}

func buildManifest() extManifest {
	cmdFields := []core.ParamSpec{
		{Name: "name", Type: core.ParamString, Required: true, Description: "Command name"},
		{Name: "description", Type: core.ParamString, Description: "Short description"},
		{Name: "script", Type: core.ParamString, Description: "Shell script contents"},
		{Name: "path", Type: core.ParamPath, Description: "Path to the script file"},
		{Name: "cwd", Type: core.ParamPath, Description: "Working directory"},
		{Name: "tags", Type: core.ParamList, Description: "Tags"},
		{Name: "author", Type: core.ParamString, Description: "Author label"},
		{Name: "status", Type: core.ParamString, Enum: []string{statusNew, statusRan, statusDismissed}, Description: "Run state"},
		{Name: "runs", Type: core.ParamInt, Description: "Run count since last reset"},
		{Name: "pinned", Type: core.ParamBool, Description: "Whether the command is pinned"},
		{Name: "detail_md", Type: core.ParamString, Description: "Fenced markdown script"},
	}
	jobFields := []core.ParamSpec{
		{Name: "number", Type: core.ParamInt, Description: "Global displayed job number"},
		{Name: "command_id", Type: core.ParamString, Description: "Command ID"},
		{Name: "command_name", Type: core.ParamString, Description: "Command name"},
		{Name: "state", Type: core.ParamString, Enum: []string{"running", "ok", "failed", "killed", "error"}, Description: "Job state"},
		{Name: "exit", Type: core.ParamString, Description: "Exit code or signal"},
		{Name: "duration", Type: core.ParamString, Description: "Elapsed duration"},
		{Name: "started", Type: core.ParamString, Description: "Start timestamp"},
		{Name: "cwd", Type: core.ParamPath, Description: "Working directory"},
		{Name: "assume_yes", Type: core.ParamBool, Description: "Ran with OCHAZUKE_ASSUME_YES=1"},
		{Name: "stdout_bytes", Type: core.ParamInt, Description: "Total stdout bytes"},
		{Name: "stderr_bytes", Type: core.ParamInt, Description: "Total stderr bytes"},
	}
	var cmds []core.CommandSpec
	for _, sp := range commandSpecs() {
		if sp.HasTag("user") {
			cmds = append(cmds, sp)
		}
	}
	return extManifest{
		Name:    "o-cmds",
		Version: version,
		Schemas: []extSchema{
			{
				Kind:           kindCommand,
				Title:          "Command",
				Plural:         "Commands",
				Fields:         cmdFields,
				DefaultColumns: []string{"name", "description", "status", "runs", "author"},
				SummaryField:   "name",
				UniqueFields:   []string{"name"},
			},
			{
				Kind:           kindJob,
				Title:          "Job",
				Plural:         "Jobs",
				Fields:         jobFields,
				DefaultColumns: []string{"number", "command_name", "state", "exit", "duration", "started"},
				SummaryField:   "command_name",
			},
		},
		Commands: cmds,
	}
}

func (a *Service) queryRecords(kind, text string) (extQueryResult, error) {
	q := strings.ToLower(strings.TrimSpace(text))
	match := func(parts ...string) bool {
		if q == "" {
			return true
		}
		for _, p := range parts {
			if strings.Contains(strings.ToLower(p), q) {
				return true
			}
		}
		return false
	}
	items := []extRecord{}
	if kind == "" || kind == kindCommand {
		rows, err := a.rows("")
		if err != nil {
			return extQueryResult{}, err
		}
		now := a.store.now()
		for _, r := range rows {
			if !match(r.Name, r.Description, strings.Join(r.Tags, " "), r.Author, r.Script) {
				continue
			}
			archive := "active"
			if r.Deleted {
				archive = "archived"
			}
			star := "unstarred"
			if r.Pinned {
				star = "starred"
			}
			updated := r.Created
			if r.Status.Last != nil && r.Status.Last.After(updated) {
				updated = *r.Status.Last
			}
			fields := r.data(now)
			fields["author"] = r.Author
			items = append(items, extRecord{
				Kind:          kindCommand,
				ID:            r.ID,
				Fields:        fields,
				ArchiveStatus: archive,
				StarStatus:    star,
				CreatedAt:     r.Created.UTC(),
				UpdatedAt:     updated.UTC(),
			})
		}
	}
	if kind == "" || kind == kindJob {
		runs, err := a.runRows("")
		if err != nil {
			return extQueryResult{}, err
		}
		for _, r := range runs {
			st := runState(r.Run)
			if !match(r.ID, r.CommandName, r.CommandID, st, r.Cwd) {
				continue
			}
			updated := r.Started
			if r.Ended != nil {
				updated = *r.Ended
			}
			dur := ""
			if r.Ended != nil {
				dur = r.Duration.Round(time.Millisecond).String()
			}
			items = append(items, extRecord{
				Kind: kindJob,
				ID:   r.ID,
				Fields: map[string]any{
					"number":       r.Number,
					"command_id":   r.CommandID,
					"command_name": r.CommandName,
					"state":        st,
					"exit":         runExit(r.Run),
					"duration":     dur,
					"started":      r.Started.UTC().Format(time.RFC3339),
					"cwd":          r.Cwd,
					"assume_yes":   r.AssumeYes,
					"stdout_bytes": r.StdoutBytes,
					"stderr_bytes": r.StderrBytes,
					"args":         r.Args,
					"source":       r.Source,
				},
				ArchiveStatus: "active",
				StarStatus:    "unstarred",
				CreatedAt:     r.Started.UTC(),
				UpdatedAt:     updated.UTC(),
			})
		}
	}
	return extQueryResult{Items: items, Total: len(items)}, nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// --- CLI subcommands --------------------------------------------------------

func (a *Service) cliList(args []string) error {
	fs := flag.NewFlagSet("o-cmds list", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	filter := fs.String("filter", "", "Match name, description or tags")
	format := fs.String("format", "table", "Output format: table, json or markdown")
	asJSON := fs.Bool("json", false, "Output JSON (shorthand for --format json)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	if *asJSON {
		*format = "json"
	}
	if fs.NArg() > 0 && *filter == "" {
		*filter = strings.Join(fs.Args(), " ")
	}
	if *format == "json" {
		rows, err := a.listRows(*filter)
		if err != nil {
			return err
		}
		return writeJSON(a.out, rows)
	}
	rows, err := a.rows(*filter)
	if err != nil {
		return err
	}
	now := a.store.now()
	head := []string{"#", "Name", "Description", "Last", "Age", "Runs", "Author"}
	trows := make([][]string, len(rows))
	for i, r := range rows {
		num := "-"
		if r.Number > 0 {
			num = strconv.Itoa(r.Number)
		}
		name := r.Name
		if r.Pinned {
			name = "★ " + name
		}
		trows[i] = []string{num, name, oneLine(r.Description), lastCell(r.Status, now), age(r.Created, now), strconv.Itoa(r.Status.Count), r.Author}
	}
	if *format == "markdown" {
		writeMarkdownTable(a.out, head, trows)
	} else {
		writePlainTable(a.out, head, trows)
	}
	return nil
}

func (a *Service) cliAdd(args []string) error {
	fs := flag.NewFlagSet("o-cmds add", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	name := fs.String("name", "", "Command name (duplicates receive a suffix)")
	desc := fs.String("desc", "", "Description")
	script := fs.String("script", "", "Script text")
	file := fs.String("file", "", "Read script from file, or - for stdin")
	cwd := fs.String("cwd", "", "Working directory")
	tagsRaw := fs.String("tags", "", "Comma-separated tags")
	author := fs.String("author", "", "Author name")
	asJSON := fs.Bool("json", false, "Print created command as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	if *name == "" && fs.NArg() >= 1 {
		*name = fs.Arg(0)
	}
	body := *script
	source, sourcePath := "text", ""
	if *file != "" {
		if *file == "-" {
			b, err := io.ReadAll(a.in)
			if err != nil {
				return err
			}
			body, source = string(b), "stdin"
		} else {
			abs, err := filepath.Abs(*file)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return err
			}
			body, source, sourcePath = string(b), "file", abs
		}
	} else if body == "" {
		if f, ok := a.in.(*os.File); !ok {
			b, err := io.ReadAll(a.in)
			if err != nil {
				return err
			}
			body, source = string(b), "stdin"
		} else if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
			b, err := io.ReadAll(a.in)
			if err != nil {
				return err
			}
			body, source = string(b), "stdin"
		}
	}
	var tags []string
	for _, t := range strings.Split(*tagsRaw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	c, err := a.addScript(Add{Name: *name, Description: *desc, Script: body, Cwd: *cwd, Tags: tags, Source: source, SourcePath: sourcePath}, *author)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(a.out, c)
	}
	fmt.Fprintf(a.out, "%s\t%s\n", c.Name, c.ID)
	return nil
}

func (a *Service) cliShow(args []string) error {
	fs := flag.NewFlagSet("o-cmds show", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	format := fs.String("format", "script", "Output format: script, json or markdown")
	asJSON := fs.Bool("json", false, "Output JSON (shorthand for --format json)")
	flags, pos, err := splitFlags(args, map[string]bool{"--json": true, "--help": true, "-h": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	if *asJSON {
		*format = "json"
	}
	if len(pos) != 1 {
		return errors.New("usage: o-cmds show NAME|NUMBER|ID [--format script|json|markdown]")
	}
	c, err := a.resolve(pos[0])
	if err != nil {
		return err
	}
	script, err := a.store.Script(c)
	if err != nil {
		return err
	}
	switch *format {
	case "json":
		return writeJSON(a.out, struct {
			Command
			Script string `json:"script"`
		}{c, script})
	case "markdown":
		_, err = io.WriteString(a.out, scriptMarkdown(script, c.Path))
		return err
	default:
		_, err = io.WriteString(a.out, script)
		return err
	}
}

var runBoolFlags = map[string]bool{"--yes": true, "--assume-yes": true, "--edit-stdin": true, "--show": true, "--help": true, "-h": true}

func splitFlags(args []string, boolFlags map[string]bool) (flags, positional []string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if !strings.Contains(arg, "=") && !boolFlags[arg] {
			i++
			if i >= len(args) {
				return nil, nil, fmt.Errorf("%s needs a value", arg)
			}
			flags = append(flags, args[i])
		}
	}
	return flags, positional, nil
}

func (a *Service) cliRun(ctx context.Context, args []string) error {
	var argv []string
	for i, arg := range args {
		if arg == "--" {
			argv = append(argv, args[i+1:]...)
			args = args[:i]
			break
		}
	}
	flags, positional, err := splitFlags(args, runBoolFlags)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("o-cmds run", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	target := fs.String("target", "", "Command name, number or ID")
	raw := fs.String("json", "{}", "Host arguments")
	encoded := fs.String("args", "", "JSON array of script arguments")
	inputPath := fs.String("stdin", "", "File to use as stdin")
	inputKind := fs.String("stdin-kind", "file", "Input provenance: file or edited")
	editInput := fs.Bool("edit-stdin", false, "Edit an empty stdin buffer")
	show := fs.Bool("show", false, "Capture and print labelled run output")
	fs.Bool("yes", false, "Host compatibility")
	assume := fs.Bool("assume-yes", false, "Run with OCHAZUKE_ASSUME_YES=1 (recorded; reruns repeat it)")
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	var host struct {
		Target    string   `json:"target"`
		Args      []string `json:"args"`
		Stdin     string   `json:"stdin"`
		EditStdin bool     `json:"edit-stdin"`
		StdinKind string   `json:"stdin-kind"`
		AssumeYes bool     `json:"assume-yes"`
		Show      bool     `json:"show"`
	}
	if err := json.Unmarshal([]byte(*raw), &host); err != nil {
		return err
	}
	assumeYes := *assume || host.AssumeYes
	if *target == "" {
		*target = host.Target
	}
	if len(positional) >= 1 {
		argv = append(append([]string{}, positional[1:]...), argv...)
		*target = positional[0]
	}
	if *encoded != "" {
		if err := json.Unmarshal([]byte(*encoded), &host.Args); err != nil {
			return err
		}
	}
	argv = append(host.Args, argv...)
	if *target == "" {
		return errors.New("name a command or displayed number")
	}
	row, err := a.resolve(*target)
	if err != nil {
		return err
	}
	if *inputPath == "" {
		*inputPath = host.Stdin
	}
	if *editInput || host.EditStdin {
		if *inputPath != "" {
			return errors.New("--stdin and --edit-stdin are mutually exclusive")
		}
		return a.runEdited(ctx, row, argv, assumeYes)
	}
	if host.StdinKind != "" && *inputKind == "file" {
		*inputKind = host.StdinKind
	}
	in, err := a.openInput(*inputPath, *inputKind)
	if err != nil {
		return err
	}
	if *show || host.Show {
		id, err := newJobID()
		if err != nil {
			in.close()
			return err
		}
		out, err := a.captureRun(ctx, row, argv, assumeYes, in, "cli", id)
		if err != nil {
			return err
		}
		_, err = io.WriteString(a.out, runText(out)+"\n")
		return err
	}
	return a.runRecordedInput(ctx, row, argv, assumeYes, in, "cli")
}

func (a *Service) cliJobs(args []string) error {
	fs := flag.NewFlagSet("o-cmds jobs", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	cmdTarget := fs.String("command", "", "Filter by command name, number or ID")
	showJob := fs.String("show", "", "Print labelled output of JOB")
	outJob := fs.String("out", "", "Print captured stdout of JOB")
	errJob := fs.String("err", "", "Print captured stderr of JOB")
	inJob := fs.String("in", "", "Print retained stdin of JOB")
	delJob := fs.String("delete", "", "Delete JOB and its captured streams")
	format := fs.String("format", "table", "Output format: table or json")
	asJSON := fs.Bool("json", false, "Output JSON")
	flags, pos, err := splitFlags(args, map[string]bool{"--json": true, "--help": true, "-h": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	if *asJSON {
		*format = "json"
	}
	if *delJob != "" {
		return a.store.DeleteRun(*delJob)
	}
	writeStreamBytes := func(job, stream string) error {
		r, err := a.store.ReadRun(job)
		if err != nil {
			return err
		}
		var b []byte
		switch stream {
		case "show":
			_, err = io.WriteString(a.out, runText(r)+"\n")
			return err
		case "out":
			if r.StdoutBase64 != "" {
				b, err = base64.StdEncoding.DecodeString(r.StdoutBase64)
			} else {
				b = []byte(r.Stdout)
			}
		case "err":
			if r.StderrBase64 != "" {
				b, err = base64.StdEncoding.DecodeString(r.StderrBase64)
			} else {
				b = []byte(r.Stderr)
			}
		case "in":
			if r.InputBase64 != "" {
				b, err = base64.StdEncoding.DecodeString(r.InputBase64)
			} else {
				b = []byte(r.Input)
			}
		}
		if err != nil {
			return err
		}
		_, err = a.out.Write(b)
		return err
	}
	switch {
	case *showJob != "":
		return writeStreamBytes(*showJob, "show")
	case *outJob != "":
		return writeStreamBytes(*outJob, "out")
	case *errJob != "":
		return writeStreamBytes(*errJob, "err")
	case *inJob != "":
		return writeStreamBytes(*inJob, "in")
	}
	if *cmdTarget == "" && len(pos) > 0 {
		*cmdTarget = pos[0]
	}
	id := ""
	if *cmdTarget != "" {
		c, err := a.resolve(*cmdTarget)
		if err != nil {
			return err
		}
		id = c.ID
	}
	rows, err := a.runRows(id)
	if err != nil {
		return err
	}
	if *format == "json" {
		return writeJSON(a.out, rows)
	}
	return writeRunTable(a.out, rows)
}

func (a *Service) cliRerun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("o-cmds rerun", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	jobFlag := fs.String("job", "", "Job ID, prefix or displayed number")
	cmdFlag := fs.String("cmd", "", "Command name, number or ID (reruns its latest job)")
	flags, pos, err := splitFlags(args, map[string]bool{"--help": true, "-h": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	if *cmdFlag != "" {
		c, err := a.resolve(*cmdFlag)
		if err != nil {
			return err
		}
		r, err := a.latestRun(c)
		if err != nil {
			return err
		}
		return a.rerunRecorded(ctx, r)
	}
	target := *jobFlag
	if target == "" && len(pos) > 0 {
		target = pos[0]
	}
	if target == "" {
		return errors.New("usage: o-cmds rerun JOB|COMMAND")
	}
	// Prefer a matching command's latest run when a command name is given;
	// fall back to a job ID / number.
	if r, err := a.store.ReadRun(target); err == nil {
		return a.rerunRecorded(ctx, r)
	}
	c, err := a.resolve(target)
	if err != nil {
		return fmt.Errorf("rerun %q: no matching job or command", target)
	}
	r, err := a.latestRun(c)
	if err != nil {
		return err
	}
	return a.rerunRecorded(ctx, r)
}

func (a *Service) cliQuery(args []string) error {
	fs := flag.NewFlagSet("o-cmds query", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	kind := fs.String("kind", "", "Filter by schema kind (cmds.command or cmds.job)")
	flags, pos, err := splitFlags(args, map[string]bool{"--help": true, "-h": true})
	if err != nil {
		return err
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	res, err := a.queryRecords(*kind, strings.Join(pos, " "))
	if err != nil {
		return err
	}
	return writeJSON(a.out, res)
}

func (a *Service) cliPick(args []string, cfg Config) error {
	fs := flag.NewFlagSet("o-cmds pick", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &ExitError{Code: 2}
	}
	if fs.NArg() > 1 {
		return errors.New("usage: o-cmds pick [NAME|NUMBER]")
	}
	if fs.NArg() == 1 {
		c, err := a.resolve(fs.Arg(0))
		if err != nil {
			return err
		}
		script, err := a.store.Script(c)
		if err != nil {
			return err
		}
		_, err = io.WriteString(a.out, script)
		return err
	}
	cfg.Pick = true
	env := newEnv(a, cfg)
	var picked string
	env.Picked = &picked
	sh := newShell(env)
	if _, err := tea.NewProgram(program{sh, env}, tea.WithInput(a.in), tea.WithOutput(a.errOut)).Run(); err != nil {
		return err
	}
	if picked == "" {
		return &ExitError{Code: 1}
	}
	_, err := io.WriteString(a.out, picked)
	return err
}

func writePlainTable(w io.Writer, head []string, rows [][]string) {
	widths := make([]int, len(head))
	for i, h := range head {
		widths[i] = textW(h)
	}
	for _, r := range rows {
		for i := range head {
			widths[i] = max(widths[i], textW(clip(r[i], 48)))
		}
	}
	line := func(cells []string) {
		var b strings.Builder
		for i := range head {
			c := clip(cells[i], 48)
			if i < len(head)-1 {
				c += strings.Repeat(" ", widths[i]-textW(c)+2)
			}
			b.WriteString(c)
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	line(head)
	sep := make([]string, len(head))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	line(sep)
	for _, r := range rows {
		line(r)
	}
}

func writeMarkdownTable(w io.Writer, head []string, rows [][]string) {
	esc := strings.NewReplacer("|", `\|`, "\r\n", "<br>", "\n", "<br>", "\r", "<br>")
	line := func(cells []string) {
		parts := make([]string, len(head))
		for i := range head {
			parts[i] = esc.Replace(cells[i])
		}
		fmt.Fprintln(w, "| "+strings.Join(parts, " | ")+" |")
	}
	line(head)
	sep := make([]string, len(head))
	for i := range sep {
		sep[i] = "---"
	}
	line(sep)
	for _, r := range rows {
		line(r)
	}
}

// --- driver & smoke ---------------------------------------------------------

// driver feeds a Shell synchronously without a TTY: every command a step
// returns runs at once and its message is fed back. Both --smoke and the
// tests use it.
type driver struct{ sh *otea.Shell }

func newDriver(sh *otea.Shell, w, h int) *driver {
	sh.Engine.PrefixTimeout = 0
	sh.MaxFPS = 0
	d := &driver{sh: sh}
	d.send(tea.WindowSizeMsg{Width: w, Height: h})
	return d
}

func (d *driver) send(msg tea.Msg) {
	_, cmd := d.sh.Update(msg)
	d.pump(cmd)
}

func (d *driver) pump(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var m tea.Msg
	select {
	case m = <-done:
	case <-time.After(5 * time.Second):
		return
	}
	switch m := m.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			d.pump(c)
		}
	case tea.QuitMsg, nil:
	default:
		d.send(m)
	}
}

func (d *driver) keys(names ...string) {
	for _, n := range names {
		d.send(keyMsg(n))
	}
}

func (d *driver) typeText(s string) {
	for _, r := range s {
		if r == ' ' {
			d.send(keyMsg("space"))
		} else {
			d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

func (d *driver) command(id string) {
	d.send(core.CommandEvent{Command: core.Command{ID: id, Source: "palette", Actor: core.Actor{Kind: "user"}}})
}

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	"delete": tea.KeyDelete,
}

func keyMsg(n string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		if rest, ok := strings.CutPrefix(n, "ctrl+"); ok && len(rest) > 0 {
			mod, n = mod|tea.ModCtrl, rest
		} else if rest, ok := strings.CutPrefix(n, "alt+"); ok && len(rest) > 0 {
			mod, n = mod|tea.ModAlt, rest
		} else if rest, ok := strings.CutPrefix(n, "shift+"); ok && len(rest) > 0 {
			mod, n = mod|tea.ModShift, rest
		} else {
			break
		}
	}
	if r, ok := namedKeys[n]; ok {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	if n == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " ", Mod: mod}
	}
	r := []rune(n)[0]
	if mod != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	return tea.KeyPressMsg{Code: r, Text: n}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[A-Za-z]|\x1b\][^\a\x1b]*(\a|\x1b\\)`)

func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

func maxLineW(s string) int {
	w := 0
	for _, l := range strings.Split(s, "\n") {
		w = max(w, textW(l))
	}
	return w
}

// runSmoke drives a full scripted 120x32 session over a temporary data dir
// through the real keymap, engine, shell and renderer, and never touches any
// real user files or the clipboard.
func runSmoke(stdout io.Writer) int {
	dir, err := os.MkdirTemp("", "o-cmds-smoke-*")
	if err != nil {
		fmt.Fprintln(stdout, "smoke:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	st := &Store{Dir: dir}
	svc := newService(st, strings.NewReader(""), io.Discard, io.Discard, nil)
	if _, err := svc.addScript(Add{Name: "hello", Description: "Greet the caller", Script: "echo \"hello from o-cmds\"\n", Tags: []string{"demo"}}, "smoke"); err != nil {
		fmt.Fprintln(stdout, "smoke:", err)
		return 1
	}
	if _, err := svc.addScript(Add{Name: "check-yes", Description: "Print OCHAZUKE_ASSUME_YES", Script: "echo \"ASSUME_YES=${OCHAZUKE_ASSUME_YES:-0}\"\n", Tags: []string{"env"}}, "smoke"); err != nil {
		fmt.Fprintln(stdout, "smoke:", err)
		return 1
	}
	if _, err := svc.addScript(Add{Name: "status", Description: "Show date and host", Script: "echo \"ok\"\n", Tags: []string{"ops"}}, "smoke"); err != nil {
		fmt.Fprintln(stdout, "smoke:", err)
		return 1
	}

	env := newEnv(svc, Config{})
	env.Refresh = 0
	sh := newShell(env)
	d := newDriver(sh, 120, 32)
	app := func() App { return appOf(sh.State) }
	frame := func() string { return plain(sh.View().Content) }

	var results []string
	fails := 0
	check := func(name string, ok bool) {
		mark := "ok  "
		if !ok {
			mark, fails = "FAIL", fails+1
		}
		results = append(results, mark+" "+name)
	}

	f := frame()
	check("frame is 120x32", len(strings.Split(f, "\n")) == 32 && maxLineW(f) <= 120)
	check("starts on Commands view with 3 commands", app().View == viewCommands && len(app().Cmds) == 3 && strings.Contains(f, "Commands 3"))
	d.keys("g", "g")
	check("g g moves to first command (hello)", app().Cmd.Grid.SelectedIdx == 0 && strings.Contains(frame(), "hello from o-cmds"))
	d.keys("j")
	check("j moves to check-yes", app().Cmd.Grid.SelectedIdx == 1)

	// Run with assume-yes (g Y) -> opens output viewer showing ASSUME_YES=1.
	d.keys("g", "Y")
	check("g Y runs with OCHAZUKE_ASSUME_YES=1", len(app().Runs) == 1 && app().Runs[0].AssumeYes && strings.Contains(frame(), "ASSUME_YES=1"))
	d.keys("esc")
	check("esc closes output viewer", len(sh.State.Modals) == 0)

	// Rerun repeats assume-yes.
	d.keys("g", "r")
	check("g r reruns and repeats assume-yes", len(app().Runs) == 2 && app().Runs[1].AssumeYes)
	d.keys("esc")

	// Filter with Readline editing.
	d.keys("/")
	d.typeText("stat")
	a1 := app()
	check("/ filters commands while typing", a1.Typing && len(a1.Cmd.Grid.Items()) == 1)
	d.keys("ctrl+u")
	a2 := app()
	check("ctrl+u clears filter via Readline", a2.Typing && a2.Cmd.Grid.Query == "" && len(a2.Cmd.Grid.Items()) == 3)
	d.keys("esc")

	// Sort cycle.
	d.keys("s")
	check("s sorts commands", app().Cmd.Sort == 0 && app().Cmd.Grid.SortDir == table.SortAsc)
	d.keys("S")
	check("S reverses sort", app().Cmd.Grid.SortDir == table.SortDesc)

	// Add a command via form, delete it, undo it.
	d.keys("a")
	check("a opens add form", len(sh.State.Modals) == 1 && sh.State.Modals[0].ID == modalForm)
	d.typeText("smoke-added")
	d.keys("tab")
	d.typeText("Created in smoke")
	d.keys("tab")
	d.typeText("echo added")
	d.keys("enter")
	check("form submit saves new command", len(sh.State.Modals) == 0 && len(app().Cmds) == 4)
	d.keys("d", "d")
	check("d d deletes focused command", len(app().Cmds) == 3)
	d.keys("u")
	check("u undoes deletion", len(app().Cmds) == 4)

	// Palette & Help.
	d.keys(":")
	check(": opens command palette", len(sh.State.Modals) == 1 && sh.State.Modals[0].ID == modalPalette)
	d.keys("esc", "?")
	check("? opens live help", len(sh.State.Modals) == 1 && strings.Contains(frame(), "cmd.run.yes"))
	d.keys("esc")

	// Switch to Jobs table and verify column headers #, Exit, Duration, State.
	d.keys("tab")
	jf := frame()
	check("tab switches to Jobs table with #, Exit, Duration, State", app().View == viewJobs &&
		strings.Contains(jf, "#") && strings.Contains(jf, "Exit") && strings.Contains(jf, "Duration") && strings.Contains(jf, "State"))

	// Geometry matches layout.
	a := app()
	rect, geo := sh.State.Layout.Rects[nodeList], env.geometry(&a, 120, 32)
	check("list geometry matches layout", rect.W == geo.tableW && rect.H == geo.tableH)

	for _, line := range strings.Split(jf, "\n") {
		fmt.Fprintln(stdout, strings.TrimRight(line, " "))
	}
	fmt.Fprintln(stdout)
	for _, r := range results {
		fmt.Fprintln(stdout, r)
	}
	fmt.Fprintf(stdout, "smoke: %d/%d checks passed · %d commands · %d jobs · %d renders\n", len(results)-fails, len(results), len(a.Cmds), len(a.Runs), sh.Renders)
	if fails > 0 {
		return 1
	}
	return 0
}
