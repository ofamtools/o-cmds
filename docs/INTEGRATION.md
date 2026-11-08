# Family integration

Command records, the recorder and script/history projections belong in
**o-cmds**. Result presentation, catalog metadata and process handoff belong in
**ochazuke**. **matomeru** uses the shared `cli.RunMounted` helper; no host
branches on a tool, noun or verb name. Hosts need the updated library and a
refreshed extension catalog to pick up this contract. nabe's separate proxy
has not been migrated.

## Mounted results

| Key | Catalog verb | Host behavior |
|---|---|---|
| `v` | `cmd.show`, result `text` | Read-only script text viewer; standalone retains its numbered script viewer |
| Enter | `cmd.run {show: true}`, result `text` | Run with the shared recorder, then read captured stdout, stderr and exit status; both streams are capped by `jobs.max-bytes` |
| `H` | `cmd.history`, `foreground: true` | Full-screen o-cmds history with the existing picker and output preview; closing its last modal returns to the host |
| `e` | `cmd.edit`, `foreground: true` | `$VISUAL`/`$EDITOR` receives a real terminal, including stdin |
| `g e` | `cmd.edit-stdin`, `foreground: true` | Editor and script run receive the terminal |
| `g i` | `cmd.run-file`, required path `stdin`, result `text` | Host's existing required-path picker, then a captured output viewer; standalone retains its native stdin picker |
| `g r` | `cmd.rerun`, `foreground: true` | Rerun with original args/cwd/stdin on the terminal; output remains in recorded history |

`cmd.run` remains a foreground streaming command for CLI and palette use.
`cmd.show` returns the script as text (the record is `cmd.get`);
`job.get` remains structured (its computed prop `output` holds the labelled streams). `job.show` returns labelled retained input,
stdout, stderr and metadata as text. No dual-form result protocol is needed.

`keys.text-results` (ochazuke, default `true`) controls automatic text viewers
on key runs. `palette.text-results=plain` disables text viewers in both paths;
`all` broadens only palette results, never ordinary key answers.
`view.jobs` still controls history, and `view.stdin` its stdin actions.
A failed script's captured output and exit status are readable; a process
start or storage failure is reported as an error. Captured host execution has
mtm's 60-second deadline. Foreground history and editor sessions have no
captured-process deadline.

The published detail uses built-in sections: metadata and fenced script
Markdown. Standalone can use the numbered script detail through
`view.detail-style=script`; catalog hosts need no o-cmds-specific detail kind.
Hosts own initial selection and refresh scheduling. `view.detail` shapes the
published catalog, so its changes require refreshing that catalog.

## Verification

`TestScriptStories` executes the app fixtures, including the formerly skipped
`host-script-viewer` and `host-foreground` stories, plus `host-stdin-file`.
`TestMountedResultStories` replays those fixtures across a real executable
boundary with mounted catalog verbs, required-path forms and the shared proxy.
The PTY smoke exercises mtm's `v`, Enter, `H`, `e`, `g i`, `g r`, and `g e`
paths and records terminal handoff frames.

Reuse: ochazuke's existing text viewer and foreground delivery, o-cmds'
`prepareRun` recorder/capped streams, history picker/`runText`, and the
previous text-results lane's interaction-snapshot tests. No owner config or
live application data is needed.
