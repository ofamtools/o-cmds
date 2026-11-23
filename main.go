// Command o-cmds records small shell scripts for the owner to inspect, run
// and review as jobs, built on Ochazuke v2.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/config"
	"ofam.dev/ochazuke/ui"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// globalData strips --data DIR from args before subcommand dispatch so it
// works in any position while leaving script arguments after -- untouched.
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
	var err error
	if dir == "" {
		dir, err = dataDir()
	} else {
		dir, err = filepath.Abs(dir)
	}
	return out, dir, err
}

const usage = `usage: o-cmds [--data DIR] [flags]
       o-cmds list [--filter TEXT] [--format table|json|markdown]
       o-cmds run NAME|NUMBER|ID [--assume-yes] [--stdin FILE|--edit-stdin] [--show] [-- ARG...]
       o-cmds add|new [--name NAME] [--desc DESC] [--script TEXT|--file FILE] [--cwd DIR] [--tags A,B]
       o-cmds show NAME|NUMBER|ID [--format script|json|markdown]
       o-cmds jobs [COMMAND] [--show|--out|--err|--in|--delete JOB] [--format table|json]
       o-cmds rerun JOB|COMMAND
       o-cmds pick [NAME|NUMBER]
       o-cmds shell-init zsh|bash [--name NAME]
       o-cmds mcp
       o-cmds catalog
       o-cmds query [TEXT] [--kind cmds.command|cmds.job]

Opens the interactive Commands & Jobs TUI when invoked with no subcommand.
Press ? for live key help, : for the command palette, tab for Jobs.

flags:
`

func run(args []string, in io.Reader, stdout, stderr io.Writer) int {
	args, dir, err := globalData(args)
	if err != nil {
		fmt.Fprintln(stderr, "o-cmds:", err)
		return 2
	}
	cfg, set := loadConfig(configDir())
	svc := newService(&Store{Dir: dir}, in, stdout, stderr, set)

	if len(args) > 0 {
		switch args[0] {
		case "-v", "--version", "version":
			fmt.Fprintf(stdout, "o-cmds %s\n", version)
			return 0
		case "--smoke":
			return runSmoke(stdout)
		case "list":
			return reportErr(stderr, svc.cliList(args[1:]))
		case "run":
			return reportErr(stderr, svc.cliRun(context.Background(), args[1:]))
		case "add", "new":
			return reportErr(stderr, svc.cliAdd(args[1:]))
		case "show":
			return reportErr(stderr, svc.cliShow(args[1:]))
		case "jobs":
			return reportErr(stderr, svc.cliJobs(args[1:]))
		case "rerun":
			return reportErr(stderr, svc.cliRerun(context.Background(), args[1:]))
		case "pick":
			return reportErr(stderr, svc.cliPick(args[1:], cfg))
		case "shell-init":
			return reportErr(stderr, shellInit(args[1:], stdout, stderr))
		case "mcp":
			return reportErr(stderr, svc.mcp())
		case "catalog":
			return reportErr(stderr, writeJSON(stdout, buildManifest()))
		case "query":
			return reportErr(stderr, svc.cliQuery(args[1:]))
		case "view":
			args = args[1:]
		}
	}

	fl := flag.NewFlagSet("o-cmds", flag.ContinueOnError)
	fl.SetOutput(stderr)
	showVer := fl.Bool("version", false, "print version and exit")
	smoke := fl.Bool("smoke", false, "run a scripted 120x32 session over a temp data dir without a TTY")
	fl.Usage = func() {
		fmt.Fprint(stderr, usage)
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVer {
		fmt.Fprintf(stdout, "o-cmds %s\n", version)
		return 0
	}
	if *smoke {
		return runSmoke(stdout)
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "o-cmds: unknown command %q\n", fl.Arg(0))
		return 2
	}
	if set.Bool("clipboard", true) {
		cfg.Copy = func(text string) error {
			return clipboard(context.Background(), text, svc.run, stderr)
		}
	}
	env := newEnv(svc, cfg)
	sh := newShell(env)
	if _, err := tea.NewProgram(program{sh, env}).Run(); err != nil {
		fmt.Fprintln(stderr, "o-cmds:", err)
		return 1
	}
	return 0
}

func reportErr(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	fmt.Fprintln(stderr, "o-cmds:", err)
	return 1
}

// configDir is $XDG_CONFIG_HOME/o-cmds, else ~/.config/o-cmds.
func configDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "o-cmds")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "o-cmds")
	}
	return ""
}

// loadConfig reads optional bindings.json, themes.json and settings.json
// (JSONC) from dir. Missing files are fine; broken ones are reported as
// notes and skipped. Nothing is ever written.
func loadConfig(dir string) (Config, *config.Settings) {
	var cfg Config
	read := func(name string) []byte {
		if dir == "" {
			return nil
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			cfg.Notes = append(cfg.Notes, name+": "+err.Error())
		}
		return data
	}
	if data := read("bindings.json"); data != nil {
		if bs, err := config.LoadBindings(data); err != nil {
			cfg.Notes = append(cfg.Notes, err.Error())
		} else {
			cfg.Bindings = bs
		}
	}
	if data := read("themes.json"); data != nil {
		if th, err := config.LoadTheme(data, ui.DefaultDarkTheme()); err != nil {
			cfg.Notes = append(cfg.Notes, err.Error())
		} else {
			cfg.Theme = &th
		}
	}
	set, err := config.LoadSettings(read("settings.json"), defaultSettings)
	if err != nil {
		cfg.Notes = append(cfg.Notes, err.Error())
		set, _ = config.LoadSettings(nil, defaultSettings)
	}
	cfg.Settings = set
	return cfg, set
}
