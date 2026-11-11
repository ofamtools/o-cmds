package main

import (
	"bytes"
	"context"
	_ "embed"
	"path/filepath"
	"slices"
	"strings"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/setup"
)

// setup.json declares o-cmds' integrations as data for ochazuke's
// shared setup verbs (o-cmds setup status|list|plan|install|uninstall):
// a managed block (ochazuke's block kind) holding the "Commands for the
// owner" note in the agents' instruction files. Both items share one marker
// pair, so two paths that resolve to one file hold one note. Follow manages
// symlink targets while keeping the links intact.
//
//go:embed setup.json
var setupItems []byte

func (a *App) declareSetup() error {
	items, err := setup.Decode(bytes.NewReader(setupItems))
	if err != nil {
		return err
	}
	opts := setup.Options{Tool: "o-cmds", Data: a.store.Dir}
	skip, err := setupSkipDefaults(items, opts)
	if err != nil {
		return err
	}
	if err := a.r.DeclareSetting(ochazuke.Setting{Name: ocli.SetupSkipSetting, Type: ochazuke.SettingList, Default: skip, Section: "setup", Description: "Setup item ids to skip; defaults omit earlier identical blocks in the same resolved directory"}); err != nil {
		return err
	}
	if err := ocli.DeclareSetup(a.r, items, opts); err != nil {
		return err
	}
	for _, source := range a.setupOperations() {
		op := source
		op.Key = ochazuke.KeyOf("cmd", "setup-"+source.Name())
		op.Builtin, op.Standard = false, ""
		if op.AppHandler != nil {
			op.Run = nil // Register installs the dispatcher under the new key.
		}
		op.Checks = nil
		for _, check := range source.Checks {
			if check.Name != "agent-rights" { // Register supplies this boundary once.
				op.Checks = append(op.Checks, check)
			}
		}
		a.r.Register(op)
	}
	return nil
}

// Co-located instruction files commonly import each other. Prefer the last
// declaration of an identical block there, without parsing harness-specific
// import syntax. This is only a default: an explicit setup.skip overrides it.
func setupSkipDefaults(items []setup.Item, opts setup.Options) ([]string, error) {
	var blocks []setup.Item
	for _, item := range items {
		if item.Kind == setup.Block && item.Follow {
			blocks = append(blocks, item)
		}
	}
	engine, err := setup.New(blocks, opts)
	if err != nil {
		return nil, err
	}
	rows, err := engine.List(context.Background())
	if err != nil {
		return nil, err
	}
	type block struct{ dir, begin, end, content string }
	seen := map[block]bool{}
	skip := []string{}
	for n := len(rows) - 1; n >= 0; n-- {
		row := rows[n]
		path, err := filepath.EvalSymlinks(row.Path)
		if err != nil {
			continue
		}
		key := block{filepath.Dir(path), row.Begin, row.End, row.Content}
		if seen[key] {
			skip = append(skip, row.ID)
		}
		seen[key] = true
	}
	return skip, nil
}

// The framework owns setup mechanics, but these items belong to this app.
// Publish only this noun's supported operations, not every framework builtin.
func (a *App) setupOperations() []ochazuke.Operation {
	noun := a.r.CanonicalNoun("ochazuke.setup")
	var out []ochazuke.Operation
	for _, op := range a.r.Operations.Entries() {
		if op.Noun() == noun && op.Unavailable == "" {
			out = append(out, op)
		}
	}
	slices.SortFunc(out, func(a, b ochazuke.Operation) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}

func (a *App) setupSpecs() []verbSpec {
	var out []verbSpec
	for _, op := range a.setupOperations() {
		out = append(out, verbSpec{noun: "cmd", name: "setup-" + op.Name(), description: op.Description, params: op.Parameters, positional: op.Positional})
	}
	return out
}
