# Changelog

## v0.4.0

- Changed: requires ochazuke v0.110.1 (esc only leaves insert in modals and `q` closes them; label-first pickers).
- Changed: the pane variables are read as `MTM_X` first, then `TUIOS_X` (mtm sets both for one release): `ENV`, `SESSION`, `PANE_ID`, `HARNESS`, `AGENT_NAME`, through one `paneEnv` helper.

## v0.3.0 (2026-11-08)

- Changed, breaking (LX8c, lexicon, one shot: no old-name aliases; every rename is in `docs/lexicon-renames-v0.105.tsv`): requires ochazuke v0.105.0. One word has one part of speech. There is no stored `state` field, so no data migration; the on-disk `runs/` directory is unchanged.
  - The noun `run` is `job` (a recorded execution): `o-cmds job list|get|show|out|err|in|rerun|delete`, MCP `cmd.job ...`, the picker `job`, the `--job` argument (was `--run`), MCP tools `job_list` and `job_show`. `run` is only a verb (`cmd run NAME`). `cmd runs` is `cmd jobs` and `cmd runs-clear` is `cmd jobs-clear` (relations of a command); `cmd run-view` is the argument `show` of `run` (`cmd run NAME --show`; Enter binds `cmd.run {show: true}`; `cmd.run` is no longer flagged foreground in the catalog, so a host captures its output, while the standalone page and the CLI still stream to the terminal).
  - `view` is `show`: `cmd show` is the script text (the viewer in the TUI), `job show` the labelled streams in the viewer. The old `show` verbs fold into the standard `get`: `cmd get NAME` is the record (the computed prop `script` holds the script), `job get ID --prop output` the labelled metadata and streams.
  - `copy` is `yank`: `cmd yank NAME` always copies the script to the clipboard and answers it (`cmd show` prints it). `cmd path` is the collection prop `path` (the storage directory) beside the thing prop `script_path`. `job out`, `in` and `err` stay operations (raw, binary-safe streams).
  - The settings follow the noun: `runs.keep` and `runs.max-bytes` are `jobs.keep` and `jobs.max-bytes`, `view.runs` is `view.jobs`, `view.run-columns` is `view.job-columns` (no old-key reader: the owner's files are rewritten from the TSV).
  - ochazuke v0.104 and v0.105 renames reached the code: `list.hide-archived` is `collection.hide-archived`, `setup status` is `setup check`, `setup plan` is `setup install --dry-run`, the store kind is `KindCollection`. The setup note names `mtm cmd jobs NAME`, `mtm cmd job list` and `cmd.job list|show|out|err`. `testdata/lexiconcheck.txt` is emptied of the framework's rows; `testdata/lexicon-words.tsv` is the family list.
- Changed (LX7b, lexicon): requires ochazuke v0.103.0. The tool declares its main noun, `cmd` (`Noun.Main()`), so the reader resolves `o-cmds add`, `o-cmds list` and `o-cmds runs NAME` to `cmd add|list|runs`. New `TestLexicon` (`lexiconcheck`) over `testdata/lexiconcheck.txt`, seeded with today's violations.
- Changed (LX7b): the "Commands for the owner (o-cmds)" setup note names the new operations: MCP `cmd.cmd add` (not `cmds.cmd add`) and the mtm CLI short forms `mtm cmd add`, `mtm cmd runs NAME`, `mtm cmd run list|show|out|err`|show|out|err` (not `mtm cmds cmd ...`). README examples use the short spellings.
- Changed: requires ochazuke v0.102.0 and oden v0.3.0 (the Operations registry and three-part keys; `cmd/opname` moved the code). o-cmds declares its namespace, `cmd` (`ochazuke.DeclareNamespace`), so its keys are `cmd.cmd.<verb>` and the catalog carries `namespace: cmd`. CLI spellings are unchanged; a host that honours the declared namespace names the MCP verbs `cmd.cmd add` instead of `cmds.cmd add`.
- Changed: the "Commands for the owner (o-cmds)" setup note now tells agents to record and read commands only through mtm (`cmds.cmd add` on the MCP gateway, `mtm cmds cmd add` on the CLI, `mtm cmds cmd runs` and `mtm cmds run ...` for what happened), never the `o-cmds` binary, and to record code and recipes as well as commands.
- Changed: cmd and run are thing nouns with stores (cmd: list, get, set, archive; run: list, get, delete). `cmd archive` is the standard archive; `cmd delete`, `cmd list`, `cmd edit`, `run list` and `run delete` stay as named verbs.
- Removed: `cmd rename` (`cmd set NAME --props '{"@label": NEW}'`) and `cmd restore` (`cmd set NAME --props '{"@archive": false}'`); no aliases. `set` names a command exactly (a name, ID or `cmd/ID`), never by prefix or displayed number.
* The "Commands for the owner (o-cmds)" setup note now says: whenever an agent asks the owner to run a command or code, it first registers it as a self-contained bash/zsh script in o-cmds and tells the owner its name. Text is identical to the owner's global instructions.
* Add `o-cmds setup status|list|plan|install|uninstall`: a managed `o-cmds` block in the Claude and Codex instruction files telling agents to record commands for the owner with `cmd add` (setting `setup.skip`, symlink-following, backups).

* Declare retained `cmd.delete` agent-never and add repeatable, undoable `cmd.archive` and its restore through the same soft-delete flag handler. Open `cmd.reset` for agents while retaining run records. Mark `run.delete` and `cmd.runs-clear` Destructive so catalog hosts and the standalone gate refuse stream loss. Mutation/undo snapshots retain the acting framework identity; original authors remain unchanged. `list.hide-archived` defaults to true and opts `mtm cmd run list|show|out|err` of hiding command rows.

* Upgrade ochazuke to v0.83.0; the script viewer uses shared modal closing and
  window-sized page/edge motions (`gg`, `G`, Ctrl-D/U/F/B), and empty run text
  follows the framework's placeholder format.

* New commands stand `mtm cmd run list|show|out|err`: a command never run and not dismissed draws in the
  tone `view.new-tone` names (default `marker.queued`; empty turns it off). The
  tone rides on the catalog rows, so mtm's Commands tab `mtm cmd run list|show|out|err`s it too. Rows carry
  `status` (new/ran/dismissed), `runs`, `last_run_at` and `last_run_age`, which
  ochazuke's prop filters offer.
* `Runs` and `Last` columns (`view.run-columns`, default on). Runs is second
  from the right (before Author) and drops last, so it `mtm cmd run list|show|out|err`s beside the detail
  pane at 120 columns; Last, then Age, then Author drop first. The count is a durable per-command counter (`runs_total` in the
  metadata), so `runs.keep` pruning or 0 never lowers it; older commands count
  their retained runs.
* New verbs `cmd dismiss` (`x`), `cmd undismiss` (`X`) and `cmd reset` (`R`,
  confirmed; resets the counter only, run records stay). They act on a `V`
  selection as one undo step; `u` undoes them, and undo never rolls the run
  counter back. Agents may dismiss/undismiss (also MCP `cmd_dismiss`,
  `cmd_undismiss`); `reset` is agent right `never`. Requires ochazuke v0.77.0.

- Animated README logo (reduced-motion users see the still logo)
* Catalog hosts (mtm Commands page, nabe) `mtm cmd run list|show|out|err` command metadata above the
  script as a fenced Markdown code block through ochazuke detail sections,
  instead of the "detail kind script is not registered" record with the
  script on one line. Rows gain `detail_md` and `author_label`. Standalone keeps
  the numbered script view; `view.detail-style=markdown` uses the host look.
  Requires ochazuke v0.75.1 (was v0.63.0).

* Run with file stdin (`cmd run --stdin FILE`) or an empty editor buffer
  (`--edit-stdin`, VISUAL/EDITOR/vi). Add binding rows g i (framework path
  chooser), g e (editor), and g r / cmd rerun (latest run); view.stdin opts `mtm cmd run list|show|out|err`
  of the picker/editor UI and view.runs covers the rerun action.
* Record stdin kind/path/bytes/SHA-256 and capped private input snapshots with
  run retention. Show them in run `mtm cmd run list|show|out|err`, raw run in and read-only MCP run_show.
  Reruns reopen original file paths or reuse retained edited input; refuse
  truncated edited reruns. Remove editor temp files on completion/cancellation.

* Capture every CLI/TUI/catalog script execution in private run history with
  separate std`mtm cmd run list|show|out|err`/stderr, status or signal, timing, arguments, cwd, runner and
  script hash. Keep live `mtm cmd run list|show|out|err`put, stdin and terminal colour detection using
  independent `mtm cmd run list|show|out|err`put PTYs; add configurable byte caps, retention and opt-out.
* Add cmd runs/runs-clear and run list/`mtm cmd run list|show|out|err`/out/err/delete/rerun, a last-run
  preview and H history chooser/`mtm cmd run list|show|out|err`put viewer (view.runs opt-out), and read-only
  MCP run_list/run_`mtm cmd run list|show|out|err`. Retain history through command deletion and undo.

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
  the Commands page, with clean std`mtm cmd run list|show|out|err` and non-zero Escape cancellation.
* Add `shell-init zsh|bash [--name NAME]`, printing a current-shell function
  that records and evaluates picked scripts; no dotfiles are changed.

* Resolve saved command names through oden's prefix/descendant matcher, after
  exact names and numbered positions. Ambiguous targets list matching commands
  with their displayed numbers and never execute or mutate a command.

* Add `o-cmds`: durable shell commands, chronological numbered lists, rich
  command verbs, retained deletion and undo, author stamps, and stdio MCP
  add/list/`mtm cmd run list|show|out|err` tools without execution.
* Publish an ochazuke Commands catalog page and standalone TUI, with binding
  rows, a newest-command starting cursor, polling and opt-`mtm cmd run list|show|out|err` settings.
