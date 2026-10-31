# Changelog

## Unreleased

* Resolve saved command names through oden's prefix/descendant matcher, after
  exact names and numbered positions. Ambiguous targets list matching commands
  with their displayed numbers and never execute or mutate a command.

* Add `o-cmds`: durable shell commands, chronological numbered lists, rich
  command verbs, retained deletion and undo, author stamps, and stdio MCP
  add/list/show tools without execution.
* Publish an ochazuke Commands catalog page and standalone TUI, with binding
  rows, a newest-command starting cursor, polling and opt-out settings.
