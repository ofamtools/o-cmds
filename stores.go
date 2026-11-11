package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"ofam.dev/ochazuke"
)

// The cmd and job nouns are thing nouns (DESIGN-001).
//
// cmd is a store over the command records: list and get read them
// (archived ones too, which the archive-role prop marks); set writes name
// (the label: the old rename, a taken name gets a suffix), pinned and
// archived (the archive role: the old archive and restore, restoring
// renames when the name was taken meanwhile); archive is the standard
// verb. delete stays the named verb: it hides a command at once (undo
// brings it back) and agents never run it. list (filter, numbered rows)
// and edit (opens $EDITOR on the script) also stay named verbs.
//
// job is a store over the run records, read and delete only: a job is a
// record of what happened, so create and set are unavailable. Its named
// verbs list and delete stay (delete is destructive and agents never run
// it); show (the viewer), out, in, err (raw, binary-safe streams) and
// rerun are job's own, and get answers the record (its computed prop
// output holds the labelled streams). A command's script is the computed
// prop script and the storage directory the collection prop path.

func (a *App) cmdStoreProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The command's stable ID"}},
		{Name: "name", Role: ochazuke.RoleLabel, Human: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The command's name; a taken name gets a suffix"}},
		{Name: "pinned", Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Pinned without moving its chronological position"}},
		{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Hidden from the default views; the script and undo history are kept"}},
		text("description", "What the command does"),
		pathProp("script_path", "The script's path (opening it never executes it)", ochazuke.PathFile),
		pathProp("cwd", "The directory it runs in", ochazuke.PathDir),
		text("created", "When it was recorded (RFC 3339)"),
		{Name: "tags", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingList, Description: "Tags"}},
		pathProp("source_path", "The original imported script file, not the stored script", ochazuke.PathFile),
		text("source", "Import provenance: file, stdin, job or command"),
		{Name: "script", ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The script's text"},
			Read: func(_ ochazuke.Ctx, t ochazuke.Thing) (any, error) {
				row, err := a.store.Resolve(fmt.Sprint(t["id"]), a.newestFirst())
				if err != nil {
					return nil, err
				}
				return a.store.Script(row)
			}},
		{Name: "path", Subject: ochazuke.SubjectCollection, Parameter: ochazuke.Parameter{Type: "path", Path: ochazuke.PathDir, Description: "The storage directory"},
			Read: func(ochazuke.Ctx, ochazuke.Thing) (any, error) { return a.store.Dir, nil }},
	}
}

func cmdThing(c Command) (ochazuke.Thing, error) {
	t, err := ochazuke.ThingOf(c)
	if err != nil {
		return nil, err
	}
	t["archived"] = c.Deleted
	t["script_path"] = c.Path
	t["path"] = filepath.Dir(c.Path)
	if c.Tags == nil {
		t["tags"] = []string{}
	}
	return t, nil
}

func (a *App) cmdStore() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.KindCollection,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			c, err := a.store.Resolve(id, a.newestFirst())
			if err != nil {
				return nil, err
			}
			return cmdThing(c)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			cs, err := a.store.records(true)
			if err != nil {
				return nil, err
			}
			if a.newestFirst() {
				slices.Reverse(cs)
			}
			out := make([]ochazuke.Thing, len(cs))
			for i, c := range cs {
				if out[i], err = cmdThing(c); err != nil {
					return nil, err
				}
			}
			return out, nil
		},
		Patch: func(ctx ochazuke.Ctx, id string, t ochazuke.Thing) (ochazuke.Thing, error) {
			a.store.Actor = ctx.Actor() // snapshots name who changed the command
			c, err := a.store.Resolve(id, a.newestFirst())
			if err != nil {
				return nil, err
			}
			name, hasName := t["name"].(string)
			if hasName && name == "" {
				return nil, errors.New("a command needs a name")
			}
			pinned, hasPin := t["pinned"].(bool)
			archived, hasArchive := t["archived"].(bool)
			v, err := a.store.Change(c.ID, func(v *Command, rows []Command) {
				if hasName {
					v.Name = uniqueName(name, rows, v.ID)
				}
				if hasPin {
					v.Pinned = pinned
				}
				if hasArchive {
					v.Deleted = archived
					if !archived {
						v.Name = uniqueName(v.Name, rows, v.ID)
					}
				}
			})
			a.refresh()
			if err != nil {
				return nil, err
			}
			return cmdThing(v)
		},
	}
}

func (a *App) jobProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The run's ID (a unique prefix or a displayed number names it too)"}},
		{Name: "command_name", Role: ochazuke.RoleLabel, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The command it ran"}},
		{Name: "command_id", Role: ochazuke.RoleOwner, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Noun: "cmd", Description: "The saved command's stable ID; empty for an unsaved line"}},
		{Name: "args", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingList, Description: "The arguments it ran with"}},
		pathProp("stdout_path", "The retained stdout capture", ochazuke.PathFile),
		pathProp("cwd", "The directory it ran in", ochazuke.PathDir),
		pathProp("stdin_path", "The original stdin file or retained edited buffer", ochazuke.PathFile),
		pathProp("input_path", "The retained stdin capture", ochazuke.PathFile),
		pathProp("stderr_path", "The retained stderr capture", ochazuke.PathFile),
		pathProp("metadata_path", "The job metadata file", ochazuke.PathFile),
		text("status", "Running, finished or failed to start"),
		text("started", "When it started (RFC 3339)"),
		text("ended", "When it ended; empty while unfinished"),
		{Name: "exit_code", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger, Description: "The exit status"}},
		text("signal", "The signal that ended it"),
		text("source", "Where the run came from"),
		{Name: "output", ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "Labelled stdin, stdout and stderr with the metadata (job show-output reads it in a viewer)"},
			Read: func(_ ochazuke.Ctx, t ochazuke.Thing) (any, error) {
				r, err := a.store.ReadRun(fmt.Sprint(t["id"]))
				if err != nil {
					return nil, err
				}
				return runText(r), nil
			}},
	}
}

func (a *App) runStore() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.KindCollection,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			rows, err := a.store.Runs("")
			if err != nil {
				return nil, err
			}
			r, err := resolveRun(rows, id)
			if err != nil {
				return nil, err
			}
			return a.jobThing(r)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			rows, err := a.store.Runs("")
			if err != nil {
				return nil, err
			}
			out := make([]ochazuke.Thing, len(rows))
			for i, r := range rows {
				if out[i], err = a.jobThing(r); err != nil {
					return nil, err
				}
				if r.Args == nil {
					out[i]["args"] = []string{}
				}
			}
			return out, nil
		},
		Delete: func(_ ochazuke.Ctx, id string) error {
			err := a.store.DeleteRun(id)
			a.refresh()
			return err
		},
		Missing: map[string]string{
			"create": "a job is a record of what happened; cmd run makes one",
			"set":    "a job is a record of what happened and does not change",
		},
	}
}

func pathProp(name, description, kind string) ochazuke.PropSpec {
	return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "path", Path: kind, Description: description}}
}

func (a *App) jobThing(r Run) (ochazuke.Thing, error) {
	t, err := ochazuke.ThingOf(r)
	if err != nil {
		return nil, err
	}
	t["stdin_path"] = r.Stdin.Path
	t["input_path"] = ""
	if r.Stdin.Kind != "" && r.Stdin.Kind != "none" {
		t["input_path"] = a.store.runPath(r, ".in")
	}
	t["stdout_path"] = a.store.runPath(r, ".out")
	t["stderr_path"] = a.store.runPath(r, ".err")
	t["metadata_path"] = a.store.runPath(r, ".json")
	t["status"] = "running"
	if r.Ended != nil {
		t["status"] = "finished"
		if r.ExitCode == nil && r.Signal == "" {
			t["status"] = "failed"
		}
	}
	return t, nil
}
