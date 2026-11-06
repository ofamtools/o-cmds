package main

import (
	"errors"
	"slices"

	"ofam.dev/ochazuke"
)

// The cmd and run nouns are thing nouns (DESIGN-001).
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
// run is a store over the run records, read and delete only: a run is a
// record of what happened, so create and set are unavailable. Its named
// verbs list and delete stay (delete is destructive and agents never run
// it); show, view, out, in, err and rerun are run's own.

func cmdStoreProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The command's stable ID"}},
		{Name: "name", Role: ochazuke.RoleLabel, Human: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The command's name; a taken name gets a suffix"}},
		{Name: "pinned", Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Pinned without moving its chronological position"}},
		{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Hidden from the default views; the script and undo history are kept"}},
		text("description", "What the command does"),
		text("cwd", "The directory it runs in"),
		text("created", "When it was recorded (RFC 3339)"),
		{Name: "tags", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingList, Description: "Tags"}},
		text("script_path", "The script's path (cmd path prints it too)"),
	}
}

func cmdThing(c Command) (ochazuke.Thing, error) {
	t, err := ochazuke.ThingOf(c)
	if err != nil {
		return nil, err
	}
	t["archived"] = c.Deleted
	t["script_path"] = c.Path
	if c.Tags == nil {
		t["tags"] = []string{}
	}
	return t, nil
}

func (a *App) cmdStore() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.Collection,
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

func runProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The run's ID (a unique prefix or a displayed number names it too)"}},
		{Name: "command_name", Role: ochazuke.RoleLabel, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The command it ran"}},
		text("command_id", "The command's stable ID"),
		{Name: "args", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingList, Description: "The arguments it ran with"}},
		text("cwd", "The directory it ran in"),
		text("started", "When it started (RFC 3339)"),
		text("ended", "When it ended; empty while unfinished"),
		{Name: "exit_code", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger, Description: "The exit status"}},
		text("signal", "The signal that ended it"),
		text("source", "Where the run came from"),
	}
}

func (a *App) runStore() ochazuke.Store {
	return ochazuke.Store{Kind: ochazuke.Collection,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			rows, err := a.store.Runs("")
			if err != nil {
				return nil, err
			}
			r, err := resolveRun(rows, id)
			if err != nil {
				return nil, err
			}
			return ochazuke.ThingOf(r)
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			rows, err := a.store.Runs("")
			if err != nil {
				return nil, err
			}
			out := make([]ochazuke.Thing, len(rows))
			for i, r := range rows {
				if out[i], err = ochazuke.ThingOf(r); err != nil {
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
			"create": "a run is a record of what happened; cmd run makes one",
			"set":    "a run is a record of what happened and does not change",
		},
	}
}
