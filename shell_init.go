package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var shellFunctionName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func shellInit(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("shell-init", flag.ContinueOnError)
	fs.SetOutput(errOut)
	name := fs.String("name", "oc", "Shell function name")
	// Keep the shell word first, allowing the documented trailing --name flag.
	shell := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		shell, args = args[0], args[1:]
	}
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if shell == "" && fs.NArg() == 1 {
		shell = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("usage: o-cmds shell-init zsh|bash [--name NAME]")
	}
	history, ok := map[string]string{"zsh": "print -s", "bash": "history -s"}[shell]
	if !ok {
		return errors.New("shell-init supports zsh or bash")
	}
	if !shellFunctionName.MatchString(*name) {
		return errors.New("function name must be a shell identifier: letters, digits and underscores, without a leading digit")
	}
	_, e := fmt.Fprintf(out, `function %s {
  local __o_cmds_script
  __o_cmds_script=$(command o-cmds pick "$@" && builtin printf '.') || return $?
  __o_cmds_script=${__o_cmds_script%%.}
  builtin %s -- "$__o_cmds_script"
  builtin eval -- "$__o_cmds_script"
}
`, *name, history)
	return e
}
