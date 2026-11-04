package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
)

func (a *App) view(c ochazuke.Ctx, row Command) ochazuke.Result {
	if a.tui {
		return a.show(c, row)
	}
	text, err := a.store.Script(row)
	return result(text, err)
}

// runView uses the same recorder as cmd.run but captures both streams for
// reading rather than handing the terminal to the script. Even a failed
// script has output worth reading. Storage/start failures remain errors.
func (a *App) runView(c ochazuke.Ctx, row Command) ochazuke.Result {
	in, err := a.openInput(ochazuke.StringArg(c.Args, "stdin", ""), "file")
	if err != nil {
		return ochazuke.Failed(err)
	}
	args := stringList(c.Args["args"])
	limit := a.r.Settings().Int("runs.max-bytes")
	out, errOut := &cappedStream{limit: limit}, &cappedStream{limit: limit}
	recorder := *a
	recorder.out, recorder.errOut = out, errOut
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := scriptCommand(runCtx, row, args)
	cmd.Stdout, cmd.Stderr = out, errOut // also capture when recording is disabled
	if in != nil {
		cmd.Stdin = in.file
	}
	finish, err := recorder.prepareRun(cmd, row, args, "viewer", in)
	if err != nil {
		cancel()
		in.close()
		return ochazuke.Failed(err)
	}
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		ctx, stopSignal := signal.NotifyContext(ctx, syscall.SIGTERM)
		defer stopSignal()
		defer in.close()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()
		defer cancel()
		runErr := a.run(cmd)
		if err := finish(runErr); err != nil {
			return nil, err
		}
		if cmd.ProcessState == nil && runErr != nil {
			return nil, runErr
		}
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		title := fmt.Sprintf("%s · exit %d", row.Name, code)
		return ochazuke.TextAnswer{Title: title, Text: fmt.Sprintf("%s\n\n--- stdout ---\n%s\n--- stderr ---\n%s", title, out.String(), errOut.String())}, nil
	}).Then(func(ochazuke.Ctx, any, error) { a.refresh() })
}

func (a *App) history(c ochazuke.Ctx, row Command) ochazuke.Result {
	if a.tui {
		return a.commandRuns(c, row)
	}
	bin, err := os.Executable()
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Foreground(exec.Command(bin, "--data", a.store.Dir, "cmd", "history", row.ID))
}

func (a *App) historyCLI(_ context.Context, words []string) error {
	target, err := a.executionTarget("cmd history", "target", words)
	if err != nil || target == "" {
		return err
	}
	row, err := a.store.Resolve(target, a.newestFirst())
	if err != nil {
		return err
	}
	if !a.r.Settings().Bool("view.runs") {
		return nil
	}
	p, err := mountPage(a)
	if err != nil {
		return err
	}
	for _, item := range p.v.List(listID).Items() {
		if item.Data == "cmd/"+row.ID {
			_ = a.r.SetFocus(item.ID)
			break
		}
	}
	// The chooser and its previews are the existing history UI. Its last
	// modal closing ends this foreground session and restores the host.
	if _, err := a.r.Execute(ochazuke.Request{Noun: "cmd", Verb: "runs", Args: ochazuke.Arguments{"target": row.ID}}); err != nil {
		return err
	}
	if a.r.TopModal() == "" {
		return fmt.Errorf("history did not open")
	}
	_, err = tea.NewProgram(historyPage{p}, tea.WithInput(a.in), tea.WithOutput(a.out)).Run()
	return err
}

type historyPage struct{ *page }

func (p historyPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := p.page.Update(msg)
	if p.a.r.TopModal() == "" {
		cmd = tea.Batch(cmd, tea.Quit)
	}
	return p, cmd
}
