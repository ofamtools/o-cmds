# Code map

* `main.go`: global data override, oden dispatch, CLI stream/output adapters.
* `app.go`: command noun, shared verbs and parameters, bindings, settings,
  catalog page and row formatting.
* `store.go`: per-command files, author stamps, lock, duplicate suffixes,
  retained deletion and undo snapshots.
* `process.go`: injectable process runner, sh/editor/clipboard commands.
* `tui.go`: mounts the catalog page, initial newest selection, refresh timer.
* `mcp.go`: curated three-tool JSON-RPC stdio server, registry-derived schemas.
* `*_test.go`: isolated store, CLI, MCP and model-dispatch behavior checks.
* `cmd/o-story/stories/cmds`: skipped host integration gaps.
