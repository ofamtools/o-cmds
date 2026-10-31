# Family integration

All implementation changes belong in **o-cmds**: this app owns command records,
file storage, author stamping and process execution. oden and ochazuke already
own CLI resolution, catalog data pages, focus, lists, filtering, settings,
terminal handoff, binding rows and hint rendering. No sibling library or owner
configuration is changed.

The script preview belongs in the app because it projects command metadata
and source text. It reuses ochazuke's detail windows for scrolling and modal
stack for viewing; keys and hints remain binding data. Sorting needs only a
binding to the existing `ochazuke.setting.cycle` verb plus an observer to reload
the list, whose stable row IDs preserve selection. No new setting verb is added.

The app follows `o-gh/main.go`, `o-gh/catalog.go`, `o-gh/tui.go` and
`o-git/app.go`: mount the runtime with `cli.Mount`, publish a `cmd` noun and
data page, and mount that same page standalone through `cli.MountPage`.
The small CLI adapter preserves literal script output and exit status while
catalog calls (`--json '{}' --yes`) use the framework's JSON representation.
The run leaf streams directly, including when invoked by a catalog host.

Config alone cannot add the requested durable script store or three-tool MCP
surface. Existing binding/settings/page rows are reused for the UI. No special
case for a particular host, script name, pane, harness or noun is introduced.

Known host gaps, verified from the current local APIs:

* The published catalog page declares built-in detail sections (a fields
  section, then a markdown section over the row field `detail_md`, the script
  in a fenced code block). Every ochazuke host draws them without registering
  an o-cmds detail kind; `host-script-view` mounts the published page with no
  kinds, as mtm does. Sections replace a detail kind in ochazuke, so the
  standalone page swaps them for kind `script` (numbered lines) unless
  `view.detail-style=markdown`. The external catalog proxy still cannot open
  this app's modal from a captured CLI show result (`host-script-viewer`,
  skipped); a shared text viewer contract belongs in ochazuke and its hosts.
* `ochazuke/cli.CatalogList` has id, noun, source and title, and has no initial
  selection policy. Standalone selects the newest command via `List.Select`.
  A mounted page starts where its host chooses. A general catalog initial
  selection policy (for example first/last) belongs in ochazuke and its hosts.
* `matomeru/cmd/mtm/extensions.go:extRun` and `nabe/mount.go:proxy` execute
  `<bin> NOUN VERB --target ID --json ARGS --yes` with captured stdout. There is
  no foreground/pane execution contract in the mounted catalog verbs. Runs
  therefore execute inline, and the host displays captured output/status.
  Interactive programs and `$EDITOR` need a terminal-aware host or the
  standalone TUI; a graphical editor configured through `$EDITOR` can work
  without a terminal. A general foreground/pane capability belongs in the
  catalog and host proxy layer, rather than branching on `o-cmds`.
* `view.detail` shapes the catalog emitted by this app. A host must refresh its
  mounted catalog to pick up layout changes. `list.refresh` and
  `list.start-newest` control the standalone page; hosts own their own timers
  and cursor policy.

Skipped o-story fixtures under `cmd/o-story/stories/cmds/` record the host
gaps for a coordinator's library follow-up. Two more fixtures record app CLI
and rendering regressions covered by executable Go tests; the shared o-story
runner has no app harness. The six `script-*.json` stories now execute through
the app adapter in `script_stories_test.go`, exercising formatting, scrolling,
viewing, sort cycling, hidden detail and picker return. These skips are not claims of passing host
integration. No owner mtm/tuios process was driven for this lane.
