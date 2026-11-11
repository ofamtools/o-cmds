package main

import (
	"errors"
	"flag"
	"io"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

type picker struct {
	page   *page
	script string
	chosen bool
}

// Selection is a binding over the same page, rather than another list or key
// handler. Normal browsing remains a separate invocation of the unchanged UI.
func mountPicker(a *App) (*picker, error) {
	p, e := mountPage(a)
	if e != nil {
		return nil, e
	}
	pick := &picker{page: p}
	a.picking = pick
	a.r.Define("cmd").Mode("normal").Bind("enter", ochazuke.Do("cmd.pick"))
	cancel := ochazuke.Do("app.quit").Guard("no-modal").Captured()
	cancel.Instance = "app"
	cancel.Keys = "esc"
	if e = a.r.Bind(cancel); e != nil {
		return nil, e
	}
	// A pager/editor opened while picking must not write terminal output into
	// the script stream captured by shell command substitution.
	p.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd {
		msg.Cmd.Stdin, msg.Cmd.Stdout, msg.Cmd.Stderr = a.in, a.errOut, a.errOut
		return tea.ExecProcess(msg.Cmd, msg.Done)
	}
	return pick, nil
}

func (a *App) pickCommand(c ochazuke.Ctx) ochazuke.Result {
	if a.picking == nil {
		if ochazuke.StringArg(c.Args, "target", "") != "" {
			return a.on(a.view)(c)
		}
		return a.foregroundSelf("cmd", "pick")
	}
	return a.on(func(_ ochazuke.Ctx, row Command) ochazuke.Result {
		script, err := a.store.Script(row)
		if err != nil {
			return ochazuke.Failed(err)
		}
		a.picking.script, a.picking.chosen = script, true
		a.r.Quit = true
		return ochazuke.Done(nil)
	})(c)
}

func (p *picker) writeSelection(out io.Writer) error {
	if !p.chosen {
		return &oden.ExitError{Code: 1}
	}
	_, e := io.WriteString(out, p.script)
	return e
}

func (a *App) pickCLI(args []string) error {
	fs := flag.NewFlagSet("pick", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if fs.NArg() > 1 {
		return errors.New("usage: o-cmds pick [NAME|NUMBER]")
	}
	if fs.NArg() == 1 {
		return a.verbCLI(a.shell(), verbSpec{noun: "cmd", name: "pick", output: "raw"}, []string{fs.Arg(0)})
	}
	out := a.out
	// Route every in-process operation to the UI stream while picking. Only
	// writeSelection owns the original stdout, after terminal teardown.
	a.out = a.errOut
	defer func() { a.out = out }()
	p, e := mountPicker(a)
	if e != nil {
		return e
	}
	defer func() { a.picking = nil }()
	if _, e = tea.NewProgram(p.page, tea.WithInput(a.in), tea.WithOutput(a.errOut)).Run(); e != nil {
		return e
	}
	return p.writeSelection(out)
}
