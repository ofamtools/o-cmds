# Changelog

## Unreleased

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
