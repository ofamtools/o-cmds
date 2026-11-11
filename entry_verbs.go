package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"ofam.dev/ochazuke"
)

// The bare CLI spellings are compatibility adapters for these catalog verbs.
// Interactive/stdio leaves still own their streams instead of JSON framing.
func (a *App) entrySpecs() []verbSpec {
	return []verbSpec{
		{noun: "cmd", name: "browse", description: "Open the Commands page", foreground: true, params: map[string]ochazuke.Parameter{"smoke": {Type: "boolean", Description: "Render a frame and exit"}, "width": {Type: "integer"}, "height": {Type: "integer"}}, handler: func(c ochazuke.Ctx) ochazuke.Result {
			args := []string{"cmd", "browse"}
			if smoke, _ := c.Args["smoke"].(bool); smoke {
				args = append(args, "--smoke")
			}
			for _, name := range []string{"width", "height"} {
				if value, ok := c.Args[name]; ok {
					args = append(args, "--"+name, fmt.Sprint(value))
				}
			}
			return a.foregroundSelf(args...)
		}, cli: func(ctx context.Context, words []string) error {
			if machineArgs(words) {
				return a.shell().Run(ctx, append([]string{"cmd", "browse"}, words...))
			}
			return runTUI(a, words)
		}},
		{noun: "cmd", name: "pick", description: "Choose a script without executing it", foreground: true, params: map[string]ochazuke.Parameter{"target": commandTarget()}, positional: []string{"target"}, handler: func(c ochazuke.Ctx) ochazuke.Result {
			if ochazuke.StringArg(c.Args, "target", "") != "" {
				return a.on(a.view)(c)
			}
			return a.foregroundSelf("cmd", "pick")
		}, cli: func(ctx context.Context, words []string) error {
			if machineArgs(words) {
				return a.shell().Run(ctx, append([]string{"cmd", "pick"}, words...))
			}
			return a.pickCLI(words)
		}},
		{noun: "cmd", name: "shell-init", description: "Print the optional Bash/Zsh current-shell function", params: map[string]ochazuke.Parameter{"shell": required("bash or zsh"), "name": param("Function name (default oc)")}, positional: []string{"shell"}, handler: func(c ochazuke.Ctx) ochazuke.Result {
			args := []string{ochazuke.StringArg(c.Args, "shell", "")}
			if name, ok := c.Args["name"].(string); ok {
				args = append(args, "--name", name)
			}
			var out bytes.Buffer
			err := shellInit(args, &out, a.errOut)
			return result(out.String(), err)
		}, text: true, output: "raw"},
		{noun: "cmd", name: "serve", description: "Serve the legacy curated command MCP protocol over stdio (the family gateway uses mcp serve)", foreground: true, handler: func(ochazuke.Ctx) ochazuke.Result { return a.foregroundSelf("cmd", "serve") }, cli: func(ctx context.Context, args []string) error {
			if machineArgs(args) {
				return a.shell().Run(ctx, append([]string{"cmd", "serve"}, args...))
			}
			if len(args) > 0 {
				return fmt.Errorf("usage: o-cmds cmd serve")
			}
			return a.mcp()
		}},
	}
}

func (a *App) foregroundSelf(args ...string) ochazuke.Result {
	bin, err := os.Executable()
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Foreground(exec.Command(bin, append([]string{"--data", a.store.Dir}, args...)...))
}
