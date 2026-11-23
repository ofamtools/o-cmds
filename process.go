package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"syscall"
)

// ExitError carries a script's exit status out of a CLI run: main exits
// with Code and prints nothing more.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Runner is the only boundary that executes user scripts, editors or clipboard
// helpers. Tests inject it for any potentially destructive script.
type Runner func(*exec.Cmd) error

// assumeYesEnv is ochazuke's family-wide standing answer to a confirm
// (the shell reads it exactly as --yes). A run set to assume yes carries it in
// its environment, for that run only.
const assumeYesEnv = "OCHAZUKE_ASSUME_YES"

func withAssumeYes(cmd *exec.Cmd, on bool) *exec.Cmd {
	if on {
		cmd.Env = append(os.Environ(), assumeYesEnv+"=1")
	}
	return cmd
}

func assumesYes(cmd *exec.Cmd) bool {
	return slices.Contains(cmd.Env, assumeYesEnv+"=1")
}

func scriptCommand(ctx context.Context, c Command, args []string) *exec.Cmd {
	if c.Line != nil {
		cmd := exec.CommandContext(ctx, "sh", append([]string{"-c", *c.Line, "o-cmds-line"}, args...)...)
		cmd.Dir = c.Cwd
		return cmd
	}
	cmd := exec.CommandContext(ctx, "sh", append([]string{c.Path}, args...)...)
	cmd.Dir = c.Cwd
	return cmd
}

func exitError(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code < 0 {
			code = 128
			if status, ok := ee.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code += int(status.Signal())
			}
		}
		return &ExitError{Code: code}
	}
	return err
}

func editorCommand(ctx context.Context, path string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// VISUAL/EDITOR is deliberately a shell command; the file remains a separate
	// positional argument, including when its path contains shell metacharacters.
	return exec.CommandContext(ctx, "sh", "-c", "exec "+editor+" \"$@\"", "o-cmds-editor", path)
}

func clipboard(ctx context.Context, text string, run Runner, stderr io.Writer) error {
	var cmd *exec.Cmd
	if custom := os.Getenv("O_CMDS_CLIPBOARD"); custom != "" {
		cmd = exec.CommandContext(ctx, "sh", "-c", custom)
	} else {
		choices := [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
		if runtime.GOOS == "darwin" {
			choices = append([][]string{{"pbcopy"}}, choices...)
		}
		for _, argv := range choices {
			if _, e := exec.LookPath(argv[0]); e == nil {
				cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
				break
			}
		}
	}
	if cmd == nil {
		return fmt.Errorf("no clipboard helper; cmd show-script NAME prints the script")
	}
	cmd.Stdin = strings.NewReader(text)
	cmd.Stderr = stderr
	return run(cmd)
}
