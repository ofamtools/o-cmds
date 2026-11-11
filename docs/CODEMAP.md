# Code map

* `main.go`: global data override, oden dispatch, CLI stream/output adapters.
* `entry_verbs.go`: registered page, script picker, shell-function and legacy
  MCP entry points; bare forms in main.go only forward to them.
* `file_verbs.go`: reader-based script import, writer-based export, script-aware
  duplicate, unsaved captured jobs and saving their scripts as named commands.
* `job_io.go`: binary-safe capture destinations, write/append, clipboard and
  sanctioned host register calls, generic external-tool opening.
* `stores.go`: typed file/directory props, key/label/archive/owner roles and
  computed capture paths; library-generated record forms and path actions.
* `pickers.go`: the cmd and run noun pickers (`PickerDef`), listed by the catalog.
* `app.go`: command noun, shared verbs and parameters, declarative agent and
  destruction metadata, transport-bound mutation identity, archive/restore flag
  handler, bindings, settings, catalog page and row formatting. Custom rows
  honour `collection.hide-archived`; archived records use names/IDs, not live numbers.
* `setup.go`, `setup.json`: agent instruction-note setup items (Claude and Codex
  instruction files, own `o-cmds` markers) for ochazuke's shared `setup` verbs,
  with co-located block skip defaults (`setup.skip`).
  `cmd.setup-*` wrappers export those exact handlers and parameters to hosts;
  bare setup routes use them too.
* `store.go`: per-command files, author stamps, lock, duplicate suffixes,
  retained deletion, archive-aware resolution, actor-attributed undo snapshots
  and receipts (batched for selections; the run counter is never rolled back)
  and the durable run counter.
* `stdin.go`: explicit input descriptors, hashing/capped snapshots, shared
  path picker with retained recent inputs, private editor buffer lifecycle.
* `process.go`: injectable process runner, sh/editor/clipboard commands.
* `run_capture.go`: shared CLI/foreground recorder, capped tee streams and
  independent output PTYs with resize and bounded draining.
* `run_store.go`: run metadata, private atomic stream files, locked reading,
  retention and deletion, independent of retained command deletion/undo.
  Unsaved lines retain their script and have no invented command reference.
* `run_state.go`: a command's run state (new/ran/dismissed, count since reset,
  last run) from the durable counter; dismiss/undismiss/reset over a selection
  as one undo step; the new-row tone and Runs/Last cells.
* `run_verbs.go`: run noun, command history relations, history picker and
  output viewer, CLI stream/table adapters and rerun with original args/cwd.
* `mounted_results.go`: titled text script/run verbs, cancellable captured
  execution through the shared recorder, and selected-row foreground history.
* `mounted_results_test.go`: host acceptance stories across real captured and
  foreground processes, including deadlines, closed records and duplicate keys.
* `tui.go`: mounts the catalog page, initial newest selection, refresh timer.
* `detail_md.go`: catalog detail sections for hosts, fenced script Markdown
  with shebang/extension language and outfenced backticks.
* `script_view.go`: script detail projection, numbered whitespace-preserving
  rendering and a read-only modal using ochazuke detail windows and bindings.
* `pick.go`: same-page selection bindings, stdout script result, cancellation.
* `shell_init.go`: optional Bash/Zsh function generation and name validation.
* `mcp.go`: curated seven-tool JSON-RPC stdio server, registry-derived schemas;
  command add/list/script-view/dismiss/undismiss and read-only job list/captures,
  never execution or reset. Legacy MCP tool names remain stable.
* `setup_test.go`: install/uninstall idempotence, symlink follow, shared files and
  skip defaults, all under temp HOME.
* `*_test.go`: isolated store, CLI, MCP and model-dispatch behavior checks.
* `file_verbs_test.go`: import/export, unsaved running/exit/capture and rerun/save,
  byte-preserving destinations, registers/clipboard and fake external openers.
* `rights_archive_test.go`: agent archive/restore/reset round trips, retained
  scripts/streams, snapshot attribution, visibility/numbering opt-out, name
  collisions and the published catalog rights/destruction contract.
* `script_stories_test.go`: executes app story fixtures against the mounted
  page and effective bindings, with optional rendered-frame artifacts.
* `host_mount_test.go`: mounts the published page as an external host does.
* `cmd/o-story/stories/cmds`: executable script, run and host UI stories.
