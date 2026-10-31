# Code map

* `main.go`: global data override, oden dispatch, CLI stream/output adapters.
* `app.go`: command noun, shared verbs and parameters, bindings, settings,
  catalog page and row formatting.
* `store.go`: per-command files, author stamps, lock, duplicate suffixes,
  retained deletion and undo snapshots.
* `process.go`: injectable process runner, sh/editor/clipboard commands.
* `tui.go`: mounts the catalog page, initial newest selection, refresh timer.
* `script_view.go`: script detail projection, numbered whitespace-preserving
  rendering and a read-only modal using ochazuke detail windows and bindings.
* `pick.go`: same-page selection bindings, stdout script result, cancellation.
* `shell_init.go`: optional Bash/Zsh function generation and name validation.
* `mcp.go`: curated three-tool JSON-RPC stdio server, registry-derived schemas.
* `*_test.go`: isolated store, CLI, MCP and model-dispatch behavior checks.
* `script_stories_test.go`: executes app story fixtures against the mounted
  page and effective bindings, with optional rendered-frame artifacts.
* `cmd/o-story/stories/cmds`: executable script UI stories and older skipped
  host integration gaps.
