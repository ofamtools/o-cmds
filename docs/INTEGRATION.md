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
| `v` | `cmd.show-script`, result `text` | Read-only script text viewer; standalone retains its numbered script viewer |
| Enter | `cmd.run {show: true}`, result `text` | Run with the shared recorder, then read captured stdout, stderr and exit status; both streams are capped by `jobs.max-bytes` |
| `H` | `cmd.history`, `foreground: true` | Full-screen o-cmds history with the existing picker and output preview; closing its last modal returns to the host |
| `e` | `cmd.edit`, `foreground: true` | `$VISUAL`/`$EDITOR` receives a real terminal, including stdin |
| `g e` | `cmd.edit-stdin`, `foreground: true` | Editor and script run receive the terminal |
| `g i` | `cmd.run-file`, required path `stdin`, result `text` | Host's existing required-path picker, then a captured output viewer; standalone retains its native stdin picker |
| `g r` | `cmd.rerun`, `foreground: true` | Rerun with original args/cwd/stdin on the terminal; output remains in recorded history |

`cmd.run` remains a foreground streaming command for CLI and palette use.
`cmd.show-script` returns script text. `job.show-output` returns labelled
retained input, stdout, stderr and metadata as text. `cmd.show` and `job.show`
are the standard stored-record forms in the UI and structured records on the
CLI. The computed props `script` and `output` remain available through `get`.

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

## Paths and output destinations

The catalog declares script/source/capture files as `path/file` and cwd/storage
directories as `path/dir`. A command's first path is its script, not its cwd;
a job's first path is stdout. `prop` selects another path for inherited reveal,
yank-path and open-with. Opening a script is never execution.

`cmd import`, `cmd export`, `cmd duplicate`, `cmd run-line`, `job save`,
`job write`, `job yank` and `job open` are ordinary catalog verbs. Reader/writer
handlers keep selection of input sources and output sinks separate from storage.
The captured recorder also runs unsaved scripts with `sh -c`, without creating
a command record. Its job retains the exact script for rerun/save.

Named register writes call the host's `ochazuke.register.set` or `append`
through `cli.HostFromEnv` when mounted. The shared core register-persistence
fix makes those subprocess writes durable and visible in the invoking host.
Without that fix, published v0.122.1 registers are process-local. No app-side
peer dispatch or persistence format is added. `register.persist`, default true
in that fix, is the opt-out.

`job open --with APPLICATION` uses the shared injectable OS opener and has an
observable external effect. Without `with`, the generic opener table decides.
Host routing to an in-process page/viewer cannot affect the invoking TUI
through a fresh subprocess; that requires a live host callback. External
OS/editor handlers work without such a callback.

Shared IO kinds were not available at adoption. Follow-up parameters are
`cmd.add.file`, `cmd.import.file`/`command`, `cmd.run.stdin`,
`cmd.run-file.stdin`, `cmd.run-line.stdin`, `cmd.export.output` and
`job.write.output`/`register`/`clipboard`/`append`. The reader/writer handlers
remain reusable when the kinds are released.

The `cmd.setup-*` actions expose this app's setup/instruction destinations
through the mounted catalog. They reuse the exact library operations and
parameters, including dry-run confirmation suppression. Bare `setup` calls
those same actions. The shared core also types `home` as a directory and an
instruction destination `path` as a file.


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
