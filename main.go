// Command o-cmds records small scripts for the owner to inspect and run.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
)

func main() {
	os.Exit(oden.Report(os.Stderr, "o-cmds", Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)))
}

// --data is a global override. Strip it before oden resolves commands so it
// works in either position, while preserving script arguments after --.
func globalData(args []string) ([]string, string, error) {
	var out []string
	dir := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			out = append(out, args[i:]...)
			break
		}
		if args[i] == "--data" {
			i++
			if i >= len(args) {
				return nil, "", errors.New("--data needs a directory")
			}
			dir = args[i]
			continue
		}
		if strings.HasPrefix(args[i], "--data=") {
			dir = strings.TrimPrefix(args[i], "--data=")
			continue
		}
		out = append(out, args[i])
	}
	var e error
	if dir == "" {
		dir, e = dataDir()
	} else {
		dir, e = filepath.Abs(dir)
	}
	return out, dir, e
}

func Main(args []string, in io.Reader, out, errOut io.Writer) error {
	args, dir, e := globalData(args)
	if e != nil {
		return e
	}
	a, e := newApp(&Store{Dir: dir}, in, out, errOut)
	if e != nil {
		return e
	}
	return dispatch(a, args)
}

func dispatch(a *App, args []string) error {
	app := oden.New("o-cmds", args, oden.WithBuiltins(), oden.WithStdio(a.in, a.out, a.errOut), oden.WithDefault(func(_ context.Context, args []string) error {
		if len(args) > 0 {
			return oden.ErrNotFound
		}
		return runTUI(a, nil)
	}))
	sh := a.shell()
	// Register the same app verbs with a thin stream adapter. Oden still
	// owns groups, aliases, help and unique prefixes; ochazuke owns calls.
	groups := map[string][]oden.Command{}
	for _, spec := range a.verbs() {
		v := spec
		params := []oden.Parameter{}
		for _, name := range a.r.Operations.At(ochazuke.Key(v.noun + "." + v.name)).ParameterOrder() {
			p := v.params[name]
			params = append(params, oden.Parameter{Name: name, Type: p.Type, Description: p.Description, Required: p.Required})
		}
		groups[v.noun] = append(groups[v.noun], oden.Command{Name: v.name, Summary: v.description, Parameters: params, Usage: "[NAME|NUMBER] [--flags]", Run: func(ctx context.Context, words []string) error {
			if v.cli != nil {
				return v.cli(ctx, words)
			}
			return a.verbCLI(sh, v, words)
		}})
	}
	// The standard verbs of the thing nouns (get, set, create, archive, ...)
	// that no named verb replaces are reachable beside the named ones.
	named := map[string]bool{}
	for _, spec := range a.verbs() {
		named[spec.noun+"."+spec.name] = true
	}
	keys := make([]string, 0, a.r.Operations.Len())
	for key := range a.r.Operations.Entries() {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		op := a.r.Operations.At(ochazuke.Key(key))
		if _, ours := groups[op.Noun()]; !ours || op.Standard == "" || named[key] {
			continue
		}
		v := verbSpec{noun: op.Noun(), name: op.Name(), description: op.Description}
		params := []oden.Parameter{}
		for _, name := range op.ParameterOrder() {
			p := op.Parameters[name]
			params = append(params, oden.Parameter{Name: name, Type: p.Type, Description: p.Description, Required: p.Required})
		}
		groups[v.noun] = append(groups[v.noun], oden.Command{Name: v.name, Summary: v.description, Parameters: params, Usage: "[NAME|NUMBER] [--flags]", Run: func(ctx context.Context, words []string) error {
			return a.verbCLI(sh, v, words)
		}})
	}
	for _, noun := range []string{"cmd", "run"} {
		app.Add(oden.Command{Name: noun, Summary: "inspect and manage " + noun + " records", Children: groups[noun]})
	}
	app.Add(oden.Command{Name: "view", Summary: "open the Commands page", Usage: "[--smoke] [--width N] [--height N]", Run: func(_ context.Context, args []string) error { return runTUI(a, args) }})
	app.Add(oden.Command{Name: "pick", Summary: "choose a command and print its script without running it", Usage: "[NAME|NUMBER]", Run: func(_ context.Context, args []string) error { return a.pickCLI(args) }})
	app.Add(oden.Command{Name: "shell-init", Summary: "print an optional current-shell command function", Usage: "zsh|bash [--name NAME]", Run: func(_ context.Context, args []string) error { return shellInit(args, a.out, a.errOut) }})
	app.Add(oden.Command{Name: "mcp", Summary: "serve command add/list/show and run list/show over stdio (no execution tool)", Run: func(_ context.Context, args []string) error {
		if len(args) > 0 {
			return errors.New("usage: o-cmds mcp")
		}
		return a.mcp()
	}})
	ocli.Mount(app, sh)
	app.HelpText += "\nGlobal: --data DIR overrides the command storage directory.\ncmd add reads stdin unless --file or --script supplies the script.\ncmd list is a table; --json '{}' requests catalog rows.\ncmd run NAME|NUMBER -- ARG... streams directly and returns the script's status."
	return app.Run(context.Background())
}

func machineArgs(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") || arg == "--format" || strings.HasPrefix(arg, "--format=") {
			return true
		}
	}
	return false
}

func (a *App) verbCLI(sh *ocli.Shell, v verbSpec, args []string) error {
	words := append([]string{v.noun, v.name}, args...)
	if machineArgs(args) || v.output == "" {
		return sh.Run(background(), words)
	}
	if v.output == "table" {
		return sh.Run(background(), append(words, "--format", "table"))
	}
	var buf bytes.Buffer
	copyShell := *sh
	copyShell.Stdout = &buf
	if e := copyShell.Run(background(), words); e != nil {
		return e
	}
	if strings.HasPrefix(v.output, "run-") {
		return a.printRunResult(v.output, buf.Bytes())
	}
	var text string
	if v.output == "script" {
		var rec struct {
			Script string `json:"script"`
		}
		if e := json.Unmarshal(buf.Bytes(), &rec); e != nil {
			return e
		}
		text = rec.Script
	} else {
		if e := json.Unmarshal(buf.Bytes(), &text); e != nil {
			return e
		}
		text += "\n"
	}
	_, e := io.WriteString(a.out, text)
	return e
}

// This leaf owns a stream rather than a JSON answer: never append a framework
// result to the script's bytes, and pass an ExitError through to oden.Report.
func (a *App) runCLI(ctx context.Context, args []string) error {
	var argv []string
	for i, arg := range args {
		if arg == "--" {
			argv = append(argv, args[i+1:]...)
			args = args[:i]
			break
		}
	}
	fs := flag.NewFlagSet("cmd run", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	target := fs.String("target", "", "Command name, number or ID")
	raw := fs.String("json", "{}", "Host arguments")
	encoded := fs.String("args", "", "JSON array of script arguments")
	inputPath := fs.String("stdin", "", "File to use as stdin")
	inputKind := fs.String("stdin-kind", "file", "Input provenance: file or edited (retained for reruns)")
	editInput := fs.Bool("edit-stdin", false, "Edit an empty stdin buffer")
	fs.Bool("yes", false, "Host compatibility")
	// flag accepts flags before positional words; move one target word to
	// the end, leaving option values paired and everything after -- intact.
	var positional []string
	var flags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if !strings.Contains(arg, "=") && arg != "--yes" && arg != "--edit-stdin" && arg != "--help" && arg != "-h" {
			i++
			if i >= len(args) {
				return fmt.Errorf("%s needs a value", arg)
			}
			flags = append(flags, args[i])
		}
	}
	if e := fs.Parse(flags); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return &oden.ExitError{Code: 2}
	}
	var host struct {
		Target    string   `json:"target"`
		Args      []string `json:"args"`
		Stdin     string   `json:"stdin"`
		EditStdin bool     `json:"edit-stdin"`
		StdinKind string   `json:"stdin-kind"`
	}
	if e := json.Unmarshal([]byte(*raw), &host); e != nil {
		return e
	}
	if *target == "" {
		*target = host.Target
	}
	if len(positional) >= 1 {
		argv = append(append([]string{}, positional[1:]...), argv...)
		*target = positional[0]
	}
	if *encoded != "" {
		if e := json.Unmarshal([]byte(*encoded), &host.Args); e != nil {
			return e
		}
	}
	argv = append(host.Args, argv...)
	if *target == "" {
		return errors.New("name a command or displayed number")
	}
	row, e := a.store.Resolve(*target, a.newestFirst())
	if e != nil {
		return e
	}
	if *inputPath == "" {
		*inputPath = host.Stdin
	}
	if *editInput || host.EditStdin {
		if *inputPath != "" {
			return errors.New("--stdin and --edit-stdin are mutually exclusive")
		}
		return a.runEdited(ctx, row, argv)
	}
	if host.StdinKind != "" && *inputKind == "file" {
		*inputKind = host.StdinKind
	}
	input, err := a.openInput(*inputPath, *inputKind)
	if err != nil {
		return err
	}
	return a.runRecordedInput(ctx, row, argv, input)
}

func (a *App) runEdited(ctx context.Context, row Command, argv []string) error {
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
	return a.runRecordedInput(ctx, row, argv, input)
}

func (a *App) runRecordedInput(ctx context.Context, row Command, argv []string, input *runInput) error {
	defer input.close()
	// A terminal sends these to the whole foreground process group. Let the
	// script receive them while the recorder survives to save its result.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGQUIT)
	defer signal.Stop(interrupts)
	cmd := scriptCommand(ctx, row, argv)
	finish, e := a.prepareRun(cmd, row, argv, "cli", input)
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
