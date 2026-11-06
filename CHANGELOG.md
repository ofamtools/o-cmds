# Changelog

## Unreleased

- Changed: cmd and run are thing nouns with stores (cmd: list, get, set, archive; run: list, get, delete). `cmd archive` is the standard archive; `cmd delete`, `cmd list`, `cmd edit`, `run list` and `run delete` stay as named verbs.
- Removed: `cmd rename` (`cmd set NAME --props '{"@label": NEW}'`) and `cmd restore` (`cmd set NAME --props '{"@archive": false}'`); no aliases. `set` names a command exactly (a name, ID or `cmd/ID`), never by prefix or displayed number.
* The "Commands for the owner (o-cmds)" setup note now says: whenever an agent asks the owner to run a command or code, it first registers it as a self-contained bash/zsh script in o-cmds and tells the owner its name. Text is identical to the owner's global instructions.
* Add `o-cmds setup status|list|plan|install|uninstall`: a managed `o-cmds` block in the Claude and Codex instruction files telling agents to record commands for the owner with `cmd add` (setting `setup.skip`, symlink-following, backups).

* Declare retained `cmd.delete` agent-never and add repeatable, undoable `cmd.archive` and its restore through the same soft-delete flag handler. Open `cmd.reset` for agents while retaining run records. Mark `run.delete` and `cmd.runs-clear` Destructive so catalog hosts and the standalone gate refuse stream loss. Mutation/undo snapshots retain the acting framework identity; original authors remain unchanged. `list.hide-archived` defaults to true and opts out of hiding command rows.

* Upgrade ochazuke to v0.83.0; the script viewer uses shared modal closing and
  window-sized page/edge motions (`gg`, `G`, Ctrl-D/U/F/B), and empty run text
  follows the framework's placeholder format.

* New commands stand out: a command never run and not dismissed draws in the
  tone `view.new-tone` names (default `marker.queued`; empty turns it off). The
  tone rides on the catalog rows, so mtm's Commands tab shows it too. Rows carry
  `status` (new/ran/dismissed), `runs`, `last_run_at` and `last_run_age`, which
  ochazuke's prop filters offer.
* `Runs` and `Last` columns (`view.run-columns`, default on). Runs is second
  from the right (before Author) and drops last, so it shows beside the detail
  pane at 120 columns; Last, then Age, then Author drop first. The count is a durable per-command counter (`runs_total` in the
  metadata), so `runs.keep` pruning or 0 never lowers it; older commands count
  their retained runs.
* New verbs `cmd dismiss` (`x`), `cmd undismiss` (`X`) and `cmd reset` (`R`,
  confirmed; resets the counter only, run records stay). They act on a `V`
  selection as one undo step; `u` undoes them, and undo never rolls the run
  counter back. Agents may dismiss/undismiss (also MCP `cmd_dismiss`,
  `cmd_undismiss`); `reset` is agent right `never`. Requires ochazuke v0.77.0.

- Animated README logo (reduced-motion users see the still logo)
* Catalog hosts (mtm Commands page, nabe) show command metadata above the
  script as a fenced Markdown code block through ochazuke detail sections,
  instead of the "detail kind script is not registered" record with the
  script on one line. Rows gain `detail_md` and `author_label`. Standalone keeps
  the numbered script view; `view.detail-style=markdown` uses the host look.
  Requires ochazuke v0.75.1 (was v0.63.0).

* Run with file stdin (`cmd run --stdin FILE`) or an empty editor buffer
  (`--edit-stdin`, VISUAL/EDITOR/vi). Add binding rows g i (framework path
  chooser), g e (editor), and g r / cmd rerun (latest run); view.stdin opts out
  of the picker/editor UI and view.runs covers the rerun action.
* Record stdin kind/path/bytes/SHA-256 and capped private input snapshots with
  run retention. Show them in run show, raw run in and read-only MCP run_show.
  Reruns reopen original file paths or reuse retained edited input; refuse
  truncated edited reruns. Remove editor temp files on completion/cancellation.

* Capture every CLI/TUI/catalog script execution in private run history with
  separate stdout/stderr, status or signal, timing, arguments, cwd, runner and
  script hash. Keep live output, stdin and terminal colour detection using
  independent output PTYs; add configurable byte caps, retention and opt-out.
* Add cmd runs/runs-clear and run list/show/out/err/delete/rerun, a last-run
  preview and H history chooser/output viewer (view.runs opt-out), and read-only
  MCP run_list/run_show. Retain history through command deletion and undo.

* Adopt the chartreuse List and run logo with reproducible production SVGs and
  byte-for-byte checks; add the family README header and an isolated VHS
  demo tape with five seeded commands and the recorded `assets/demo.gif`.

* Add six o-cmds logo concepts and an inline light/dark preview generator;
  leave production assets unselected.

* Preview scripts with line numbers, preserved spacing and a compact metadata
  header; scroll the detail pane with Ctrl-D/Ctrl-U or hide it with view.detail.
* View scripts in a scrollable in-app modal; cycle list.order with a binding
  row while keeping the selected command, including in pick mode.

* Add optional `pick` mode: print a named/numbered script or select one from
  the Commands page, with clean stdout and non-zero Escape cancellation.
* Add `shell-init zsh|bash [--name NAME]`, printing a current-shell function
  that records and evaluates picked scripts; no dotfiles are changed.

* Resolve saved command names through oden's prefix/descendant matcher, after
  exact names and numbered positions. Ambiguous targets list matching commands
  with their displayed numbers and never execute or mutate a command.

* Add `o-cmds`: durable shell commands, chronological numbered lists, rich
  command verbs, retained deletion and undo, author stamps, and stdio MCP
  add/list/show tools without execution.
* Publish an ochazuke Commands catalog page and standalone TUI, with binding
  rows, a newest-command starting cursor, polling and opt-out settings.
