# Changelog

## Unreleased

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
