// Command o-cmds records small shell scripts for the owner to inspect, run
// and review as jobs, built on Ochazuke v2.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/config"
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/oden"
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

	app := newOdenApp(args, svc, cfg, set, in, stdout, stderr)
	return reportErr(stderr, app.Run(context.Background()))
}

func newOdenApp(args []string, svc *Service, cfg Config, set *config.Settings, in io.Reader, stdout, stderr io.Writer) *oden.App {
	runTUI := func(_ context.Context, rootArgs []string) error {
		if len(rootArgs) > 0 {
			return &oden.ExitError{Code: 2, Err: fmt.Errorf("unknown flag or argument %q", rootArgs[0])}
		}
		c := cfg
		if set.Bool("clipboard", true) {
			c.Copy = func(text string) error {
				return clipboard(context.Background(), text, svc.run, stderr)
			}
		}
		env := newEnv(svc, c)
		sh := newShell(env)
		if _, err := tea.NewProgram(program{sh, env}).Run(); err != nil {
			return err
		}
		return nil
	}

	info := cli.AppInfo{
		Name:     "o-cmds",
		Version:  version,
		Summary:  "Record, inspect, and run small shell scripts as tracked jobs",
		HelpText: usage,
		Modes:    func() []core.Mode { return modes },
		Bindings: func() []core.Binding { return defaultBindings() },
		Specs:    func() []core.CommandSpec { return commandSpecs() },
		RunTUI:   runTUI,
		RunSmoke: func(out io.Writer, _ bool) error {
			if code := runSmoke(out); code != 0 {
				return &oden.ExitError{Code: code, Err: errors.New("smoke failed")}
			}
			return nil
		},
		Manifest: cmdsExtManifest(),
		Capabilities: &oden.Capabilities{
			Name:         "o-cmds",
			Version:      version,
			Summary:      "Record, inspect, and run small shell scripts as tracked jobs",
			Capabilities: []string{"query", "catalog", "tui", "mcp"},
		},
		Query: func(_ context.Context, kind string, qargs []string) (oden.ExtQueryResult, error) {
			res, err := svc.queryRecords(kind, strings.Join(qargs, " "))
			if err != nil {
				return oden.ExtQueryResult{}, err
			}
			items := make([]oden.ExtRecord, 0, len(res.Items))
			for _, it := range res.Items {
				items = append(items, oden.ExtRecord{
					Kind:          it.Kind,
					ID:            it.ID,
					Fields:        it.Fields,
					ArchiveStatus: it.ArchiveStatus,
					StarStatus:    it.StarStatus,
					CreatedAt:     it.CreatedAt,
					UpdatedAt:     it.UpdatedAt,
				})
			}
			return oden.ExtQueryResult{
				Items:     items,
				Total:     res.Total,
				FilterErr: res.FilterErr,
			}, nil
		},
	}

	app := cli.NewApp(info, args, oden.WithStdio(in, stdout, stderr))

	app.Add(
		oden.Command{
			Name:    "list",
			Summary: "list recorded commands",
			Usage:   "[--filter TEXT] [--format table|json|markdown]",
			Handle:  func(c *oden.Ctx) error { return svc.cliList(c.Args) },
		},
		oden.Command{
			Name:    "run",
			Summary: "run a recorded command and record its job",
			Usage:   "NAME|NUMBER|ID [--assume-yes] [--stdin FILE|--edit-stdin] [--show] [-- ARG...]",
			Handle:  func(c *oden.Ctx) error { return svc.cliRun(c.Context, c.Args) },
		},
		oden.Command{
			Name:    "add",
			Aliases: []string{"new"},
			Summary: "record a new shell command",
			Usage:   "[--name NAME] [--desc DESC] [--script TEXT|--file FILE] [--cwd DIR] [--tags A,B]",
			Handle:  func(c *oden.Ctx) error { return svc.cliAdd(c.Args) },
		},
		oden.Command{
			Name:    "show",
			Summary: "print a command's script or metadata",
			Usage:   "NAME|NUMBER|ID [--format script|json|markdown]",
			Handle:  func(c *oden.Ctx) error { return svc.cliShow(c.Args) },
		},
		oden.Command{
			Name:    "jobs",
			Summary: "list jobs or inspect captured job streams",
			Usage:   "[COMMAND] [--show|--out|--err|--in|--delete JOB] [--format table|json]",
			Handle:  func(c *oden.Ctx) error { return svc.cliJobs(c.Args) },
		},
		oden.Command{
			Name:    "rerun",
			Summary: "rerun a recorded job or a command's latest job",
			Usage:   "JOB|COMMAND",
			Handle:  func(c *oden.Ctx) error { return svc.cliRerun(c.Context, c.Args) },
		},
		oden.Command{
			Name:    "pick",
			Summary: "interactively pick a command and print its script",
			Usage:   "[NAME|NUMBER]",
			Handle:  func(c *oden.Ctx) error { return svc.cliPick(c.Args, cfg) },
		},
		oden.Command{
			Name:    "shell-init",
			Summary: "print zsh/bash shell integration helper",
			Usage:   "zsh|bash [--name NAME]",
			Handle:  func(c *oden.Ctx) error { return shellInit(c.Args, stdout, stderr) },
		},
		oden.Command{
			Name:    "mcp",
			Summary: "run the MCP stdio server (read/write commands, never runs)",
			Handle:  func(c *oden.Ctx) error { return svc.mcp() },
		},
		oden.Command{
			Name:    "view",
			Aliases: []string{"tui"},
			Summary: "open the interactive Commands & Jobs TUI",
			Handle:  func(c *oden.Ctx) error { return runTUI(c.Context, c.Args) },
		},
	)

	return app
}

func cmdsExtManifest() *oden.ExtManifest {
	m := buildManifest()
	schemas := make([]oden.ExtSchema, 0, len(m.Schemas))
	for _, s := range m.Schemas {
		fields := make([]oden.ExtField, 0, len(s.Fields))
		for _, f := range s.Fields {
			fields = append(fields, oden.ExtField{
				Name:        f.Name,
				Type:        string(f.Type),
				Required:    f.Required,
				Description: f.Description,
				Enum:        f.Enum,
				Default:     f.Default,
				RefKinds:    f.RefKinds,
			})
		}
		schemas = append(schemas, oden.ExtSchema{
			Kind:           s.Kind,
			Title:          s.Title,
			Plural:         s.Plural,
			Fields:         fields,
			DefaultColumns: s.DefaultColumns,
			SummaryField:   s.SummaryField,
			UniqueFields:   s.UniqueFields,
		})
	}
	cmds := make([]oden.ExtCommand, 0, len(m.Commands))
	for _, cmd := range m.Commands {
		params := make([]oden.ExtField, 0, len(cmd.Params))
		for _, p := range cmd.Params {
			params = append(params, oden.ExtField{
				Name:        p.Name,
				Type:        string(p.Type),
				Required:    p.Required,
				Description: p.Description,
				Enum:        p.Enum,
				Default:     p.Default,
				RefKinds:    p.RefKinds,
			})
		}
		cmds = append(cmds, oden.ExtCommand{
			ID:          cmd.ID,
			Description: cmd.Description,
			Params:      params,
			Tags:        cmd.Tags,
			Meta:        cmd.Meta,
		})
	}
	return &oden.ExtManifest{
		Name:     m.Name,
		Version:  m.Version,
		Schemas:  schemas,
		Commands: cmds,
	}
}

func reportErr(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	var oee *oden.ExitError
	if errors.As(err, &oee) {
		if oee.Err != nil {
			fmt.Fprintln(stderr, "o-cmds:", oee.Err)
		}
		return oee.Code
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
