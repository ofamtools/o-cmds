<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-animated-dark.svg">
    <img alt="o-cmds" src="assets/logo-animated-light.svg" width="360">
  </picture>
  <br>
  <a href="https://github.com/ofamtools/o-cmds/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/ofamtools/o-cmds"></a>
  <a href="https://pkg.go.dev/ofam.dev/o-cmds"><img alt="Go Reference" src="https://pkg.go.dev/badge/ofam.dev/o-cmds.svg"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ofamtools/o-cmds"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/ofamtools/o-cmds"></a>
</p>

<p align="center">Agents leave small scripts; you list, inspect and run them, with the newest at the bottom.</p>

<p align="center"><img alt="List saved commands, run one by number, then preview and run one in the TUI" src="assets/demo.gif" width="720"></p>

`o-cmds` keeps shell scripts for the owner to inspect and run by name or number.
Agents can add them through the CLI or MCP. Commands list in creation order:
**the newest is closest to the bottom**. Adding a duplicate name records another
command with a suffix (`hello-2`, etc.). Empty scripts and missing descriptions
or working directories are accepted.

`o-cmds` is an o-family extension: oden dispatches the CLI, ochazuke supplies the
TUI, settings, bindings, and catalog. An `o-cmds` executable on PATH is discoverable
by the family's `o` and `mtm` commands, just like `o-gh` or `o-git`.

Install with the Go version in [go.mod](go.mod) or newer:

```sh
go install ofam.dev/oden/cmd/o@latest   # oden's o: o cmds runs o-cmds, no dash needed
go install ofam.dev/o-cmds@latest
```

With the installed binary on PATH:

```sh
printf 'printf "hello %%s\\n" "$1"\n' | o cmds add --name hello --desc 'Say hello'
printf 'pwd\n' | o cmds add --name where --desc 'Show the current directory'
o cmds list
o cmds show-script hello
o cmds cmd run hello -- 'Ada Lovelace'
o cmds cmd run 2                 # names or displayed numbers: NAME|N
o cmds                           # standalone TUI, selected on newest
o cmds pick hello                # print a script without running it
o cmds pick                      # choose a script in the TUI
o cmds shell-init zsh            # print an optional current-shell helper
o cmds mcp                       # serve add/list/show over stdio JSON-RPC
```

Use `cmd add --file ./some-script.sh --name inspect --cwd /path/to/project`
to record a file and its working directory. `o cmds catalog` prints the JSON
catalog for mtm/nabe; `o cmds view --smoke` renders one frame without a terminal.
The [demo tape](docs/vhs/README.md) records the header animation.

`cmd list` prints numbered columns: name, description, age, author. Numbers are
positions in the current order; filtering preserves the unfiltered numbers.
Names take precedence over numeric positions. Stable `cmd/ID` targets from JSON
rows remain usable across renames and changes to the list.

Script-targeting verbs and legacy MCP `cmd_show` accept a unique name
prefix: `cmd show-script play` can select `play-o-json-tutorials`. Matching reuses
oden's resolver, including its descendant rule: a prefix matching both `play`
and `play-tutorials` selects `play`. Exact names and valid numbered positions
take precedence. Ambiguous input prints the matching commands with their
current list numbers and returns a failure; it never runs or changes a command.

The pinned oden v0.5.1 supports exact/prefix matching here. Dash-segment forms
(`p-o-j-t`, `pojt`) and last-resort word fragments require a shared oden matcher
extension; they are not implemented by a separate app matcher.

```sh
o cmds cmd show hello            # record; the UI opens its standard form
o cmds cmd show-script hello     # print the script without running it
o cmds cmd yank hello            # copy the script to the clipboard (and print it)
EDITOR='code --wait' o cmds cmd edit hello
o cmds cmd set hello --props '{"@label":"greeting"}'   # rename
o cmds cmd pin greeting          # show a star, keep chronological position
o cmds cmd star greeting         # toggle the pin
o cmds cmd unpin greeting
o cmds cmd delete greeting       # hide; keep script and history
o cmds cmd undo                  # restore the most recent change
o cmds cmd archive greeting      # agents hide without deleting
o cmds cmd set greeting --props '{"@archive":false}'   # restore the retained command
o cmds cmd get greeting --prop script_path   # script path
o cmds cmd path                  # storage directory (a collection prop)
o cmds list --filter deploy
o cmds list --json '{}'      # catalog rows, including script/metadata
o cmds list --format table   # framework table output
o cmds add --name report --script 'pwd' --tags '["work","daily"]'
```

Runs use `sh SCRIPT ARG...`, with the recorded cwd if present. Otherwise they
inherit the caller's directory. Stdin remains connected unless an explicit input
source is chosen; stdout and stderr stream live, and
the CLI returns the script's exit status. A missing recorded directory does
not prevent saving; running reports the directory error. Script bytes are
preserved, including when the script is empty or has no trailing newline.

## Script input and captured output

Importing, opening and executing are separate actions. `cmd import FILE`
records a copy without running it. It keeps the original filename in
`source_path`; the stored script has its own `script_path`. A missing cwd
does not prevent recording. File reads and writes run as pending work.

```sh
o cmds cmd import ./task.sh --name task --cwd /path/to/project
printf 'printf hello\n' | o cmds cmd import - --name piped
o cmds cmd import --command "printf 'printf generated\\n'" --name generated
o cmds cmd export task ./copy.sh
o cmds cmd export task ./copies.sh --append
o cmds cmd export task -         # exact script bytes on stdout
o cmds cmd duplicate task task-copy
o cmds cmd open task             # open the script file; never execute it
o cmds cmd reveal task
o cmds cmd yank-path task --prop script_path
```

`cmd import --command` captures the producer as a job and retains its ID in
the imported record. A failed producer or truncated output is not silently
imported; the producer's job remains available for inspection.

`cmd run-line --line 'COMMAND'` runs without saving a command first. Its
JSON answer is the completed job, including stdout, stderr and the exit code.
A nonzero script exit is recorded in `exit_code`, not treated as a failed
capture. `job list` and `job show` expose the record while it runs.
`job save JOB NAME` records its exact unsaved script under a name afterwards.

```sh
o cmds cmd run-line --line 'cat; printf "warning\n" >&2' --stdin ./input.txt
o cmds job show JOB_ID            # standard record/form
o cmds job show-output JOB_ID     # labelled captures in a viewer
o cmds job save JOB_ID saved-task
o cmds job write JOB_ID ./result.json
o cmds job write JOB_ID ./errors.log --stream stderr --append
o cmds job write JOB_ID -         # binary-safe stdout
o cmds job yank JOB_ID            # stdout to the clipboard
o cmds job write JOB_ID --stream stderr --register a
o cmds job write JOB_ID --register a --append
o cmds job open JOB_ID --with TextEdit
```

The stream selector is `stdout`, `stderr` or retained `stdin`. File and
stdout transfers preserve binary bytes. Clipboard and register transfers
require UTF-8. The three destinations `output`, `clipboard` and `register`
are mutually exclusive; append requires a file or a named register.
`job open` uses the generic opener table, and `--with` names an actual
external application. In-host page/viewer routing needs a live host callback;
a fresh host subprocess cannot change the invoking TUI's page.

Named register output uses the framework's `register set`/`append` verbs.
Mounted calls go through the sanctioned host transport, not a peer binary.
Durability and live-host refresh come from ochazuke's named-register
persistence (v0.124.0); `register.persist=false` opts out.
The app accepts only named registers `a-z` and uppercase aliases. Clipboard
output is a separate explicit destination.

The shared input/output kinds are not part of published ochazuke v0.124.0.
Until their adoption, use `import --file` or `--command` and the explicit
output parameters above. The handlers take readers and writers so adopting
the shared kinds does not require another stream parser.

`cmd show` and `job show` are the library's standard record forms in the UI
and structured records on the CLI. Their old viewers remain registered as
`cmd show-script` and `job show-output`; the `v` binding follows show-script.
The binary entry points are registered as `cmd browse`, `cmd pick`,
`cmd shell-init` and `cmd serve`. Bare `view`, `pick`, `shell-init` and legacy
`mcp` call those actions. The family gateway uses the generic `mcp serve`
catalog interface, which reaches every action subject to ordinary rights.

The app's setup items also travel in its catalog. `cmd setup-list`,
`cmd setup-check`, `cmd setup-show`, `cmd setup-install --dry-run` and the
other `setup-*` actions reuse the shared setup operations, parameters,
confirmations and filesystem engine. Bare `setup` forms call the same actions.
No separate installer or instruction-file writer exists in the app.

## Setup: the agents' note

`o-cmds setup` adds a managed block to the agents' instruction files
(`${CLAUDE_CONFIG_DIR:-~/.claude}/CLAUDE.md`, `~/.codex/AGENTS.md`) between
`<!-- o-cmds:begin -->` and `<!-- o-cmds:end -->`. It tells agents to record
commands they give you, through mtm only: `cmd.cmd add` on the mtm MCP gateway
or `mtm cmd add` on the CLI, with self-contained scripts and no secrets or
host details. Agents never call the `o-cmds` binary; you run commands with
`o-cmds cmd run`.

```sh
o cmds setup                  # status
o cmds setup install --dry-run   # dry run
o cmds setup install --yes
o cmds setup uninstall --yes
```

Symlinked instruction files are followed (links stay intact), each target gets
a `.o-cmds.bak` backup, and reinstall is idempotent. `setup.skip` lists item ids
to leave out; its default skips an earlier identical block in the same resolved
directory, so a `CLAUDE.md` importing `AGENTS.md` does not read the note twice.
mtm's Extensions page shows `setup` for o-cmds through the shared catalog noun.

## Stdin from a file or editor

```sh
o cmds cmd run hello --stdin ./input.txt -- argument
o cmds cmd run hello --edit-stdin -- argument
o cmds cmd rerun hello             # latest run, including its input source
o cmds job in 1                    # exact retained input bytes
```

On a command row, `g i` opens ochazuke's file picker at the caller's current
working directory. Its browse, recent and typed-path sources provide navigation
and path completion; recent includes file inputs from retained runs as well as
paths picked this session. Escape cancels. `g e` opens an empty private temp file
in `$VISUAL`, else `$EDITOR`, else `vi`. Editor commands may include arguments
(such as `code --wait`). Save and quit to run with that file as stdin. An empty
buffer or a nonzero editor exit cancels with a message and creates no run.
The editor controls its own writes; there is no extra app save step. Buffers
always start empty; automatic seeding is not enabled. `view.stdin=false` hides
the picker/editor binding hints and disables those TUI flows; CLI access remains.

`g r` repeats the focused command's latest recorded run. Both that action and
`job rerun JOB` reuse the original arguments, cwd and stdin source: file input
reopens the same absolute path (so it sees current file contents), while edited
input reuses the run's retained buffer. A missing source fails clearly. A
truncated edited buffer cannot be rerun, since its original bytes are no longer
available. `view.jobs=false` also hides and disables the `g r` history action.
The catalog's `cmd.run` supports `stdin`, `edit-stdin`, and `stdin-kind`
(`file` by default; `edited` snapshots an existing buffer for reuse).

Explicit inputs must be regular files. They replace the script's stdin: an
interactive script cannot read the keyboard through stdin while consuming one.
Without either option, stdin stays inherited. The editor still gets the terminal
before the script starts. Input paths are resolved from the caller's directory,
independently of the script's recorded cwd. Temporary editor files are created
as `0600` under the data directory's private `tmp/` and removed after use.

Compared with Vim: this resembles writing a buffer to `:write !command`
(`:help :write_c`); o-cmds sends the whole saved buffer or chosen file and records
the run, while Vim can send a selected range. Output is streamed and captured
rather than replacing the edited buffer as `:range!` would.

## New commands and run counts

A command that has never run and was not dismissed is **new**: its row draws in
the tone `view.new-tone` names (default `marker.queued`, the theme's accent;
any tone name works, so `view.new-tone=cmds.new` plus a `cmds.new` row in
`themes.json` gives it a colour of its own; empty turns the styling off). The
tone travels on the catalog rows, so mtm's Commands tab shows it too.

Each row carries `status` (`new`, `ran` or `dismissed`), `runs` (runs since the
last reset), `last_run_at` and `last_run_age`; every row field is a prop filter
in ochazuke, so `z /` can filter `status = new`. With `view.job-columns`
(default on, read at start) the table reads `# Name Description Last Age Runs
Author`: `Runs` is second from the right, before `Author`, and is the last of
the optional columns to drop, so at 120 columns with the script pane open the
list shows `# Name Description Runs`. Narrower lists drop `Last`, then `Age`,
then `Author`.

The count is durable: each started run adds one to a counter in the command's
metadata, whatever `jobs.keep` retains or prunes, and undo never lowers it.
Commands recorded before the counter count their retained run records.

```sh
o cmds cmd dismiss greeting      # x: mark seen without running (not new)
o cmds cmd undismiss greeting    # X: new again
o cmds cmd reset greeting --yes  # R: run count to 0, new again (asks first)
o cmds cmd undo                  # u: restores either
```

`x`, `X` and `R` act on every row of a `V` selection as one undo step. `reset`
only resets the counter: retained run records stay (`cmd jobs-clear` deletes
them), and undoing a reset restores the old count plus any runs since.

Agents may read and write, including `cmd.reset`; an allowed agent call passes
its confirmation prompt. `cmd.delete` is declared agent-never even though it
only hides. Use `cmd.archive` and `cmd.set` with `@archive` false, which share the retained flag
handler. `job.delete` and `cmd.jobs-clear` are destructive and agent-never
because they remove captured streams. Owner `agents.rights` rules still apply.

`collection.hide-archived` defaults to true; false includes archived command rows.
Archived rows have `-` in the number column, leaving live positional numbers
unchanged. Target archived commands by name or stable ID. Archive and restore
are repeatable and undoable. Restore adds a suffix if the name is occupied.

## Run history

Every script executed by `cmd run`, the standalone TUI, or the catalog's
`cmd.run` operation gets a run record. `pick` and the `oc` shell function only
print a script for the caller's shell to evaluate; **their executions cannot
be captured by o-cmds**. Shell command history and captured run history are
separate things.

```sh
o cmds cmd jobs hello             # command name, abbreviation or number
o cmds job list                   # all jobs (runs), including deleted commands
o cmds job get 1 --prop output    # metadata and labelled stdin/stdout/stderr
o cmds job out 1                  # exact captured stdout, for piping
o cmds job err 1                  # exact captured stderr, for piping
o cmds job rerun 1                # current script, original args, cwd and input
o cmds job delete 1               # remove record and captured streams
o cmds cmd jobs-clear hello       # remove all of this command's jobs
```

Runs list oldest first, newest at the bottom, independently of `list.order`.
Their numbers refer to the global run list, including when filtered by command;
numbers change after pruning/deletion. A stable `run/ID`, exact ID or unique ID
prefix also works. Ambiguous prefixes fail without executing anything.
`job list NAME|N` filters by command. `job show-output/in/out/err/delete/rerun`
accept `--job JOB` as well as a positional target; standard `job show` takes
the record ID. Machine clients can use `--json`. Saved-command reruns use
the current script and its current hash. Unsaved-line reruns use the exact
script retained in the job. Both preserve arguments, cwd and input provenance.

The `stdin` metadata object records `kind` (`none`, `file`, `edited`), source
`path`, full `bytes` and SHA-256 `sha`, plus `kept` and `truncated`. These describe
the source when execution is prepared, even if the script reads only part of it.
Inherited input is never copied. Explicit file and editor inputs retain a prefix
under the same `jobs.max-bytes` cap as output; `.in` files are deleted with their
run by retention, deletion or clearing. Edited paths point to that private kept
file. `job get --prop output` labels retained stdin and includes the metadata; `job in` prints
the exact retained bytes, including binary input. JSON includes `input` and,
when needed, `input_base64`; MCP `job_show` includes input text/base64 only up to
64 KiB, always keeping source metadata. `jobs.keep=0` disables saved-command
history; unsaved lines still retain their only durable record.

Records include the command ID and name at execution, SHA-256 script hash,
arguments, effective cwd, UTC start/end, duration, exit code or signal, runner's
author/harness stamp, CLI/TUI source, observed and retained byte counts, and
separate truncation flags. Start failures have an error and no exit code.
An interrupted recorder leaves an unfinished record; its in-memory output
cannot be recovered after a hard kill. Streams become available at completion.
`job get --prop output --json '{}'` and MCP return readable stream text; a binary stream
also has `stdout_base64` or `stderr_base64` to preserve invalid UTF-8 exactly.

Each terminal output stream gets its own output PTY; redirected output uses
tee pipes. Terminal detection, ANSI colours, initial dimensions and resize
updates are preserved; stdout and stderr remain separate. Stdin and the
controlling terminal are inherited. Independent output PTYs mean scripts
that change terminal modes through stdout, compare tty device identities, or
write directly to `/dev/tty` can behave differently. Direct `/dev/tty` writes
bypass capture. There is no combined cross-stream ordering guarantee. A
background descendant retaining an output descriptor gets a bounded drain
(one second per PTY; one second for exec's pipe drain); `capture_error` or the
run's error marks an incomplete drain. Ordinary interactive input and colours
continue to work. `jobs.keep=0` restores direct output without recording.

History lives in `runs/<command-id>/<run-id>.json`, `.out`, `.err`, and optional
`.in` files under
the data directory. Files are `0600`, new directories `0700`; atomic writes
and the command store's process lock protect readers and writers. An unfinished
record is published before execution (including the retained explicit input),
and final metadata after output streams.
Command deletion retains runs, matching retained scripts and undo; run deletion
and `runs-clear` permanently remove history and are not command undo operations.
Clearing a running record prevents completion from recreating it.

Defaults are `jobs.keep=20` completed runs per command, `jobs.max-bytes=1048576`
per stream, `view.jobs=true`, and `view.stdin=true`. Retention applies on
completion; active and
unfinished records are kept. A cap keeps the beginning of each stream while
continuing to count and forward every byte. Zero cap retains metadata only.
`jobs.keep=0` disables future recording, retaining existing records. The settings
are nonnegative integers. `view.jobs=false` hides the last-run status and disables
the history modal without disabling recording or CLI access.

The command preview shows the latest run's status, time and duration. `H` opens
the focused command's history, with the framework picker's binding-derived
hints and output preview. Its normal search and movement bindings apply;
Enter opens the selected run in the scrollable viewer. Escape returns to the
run list, then another Escape returns to the host. `history.keep-open=false`
restores closing the list when a run opens. The backdrop selects that command.

In mtm, `H` runs `cmd.history` as a foreground full-screen session and closing
it returns to the host. `v` uses `cmd.show-script` (declared text), and Enter uses
`cmd.run {show: true}` (`cmd run NAME --show`) to open captured stdout, stderr and exit status in the host's
read-only viewer. `keys.text-results=false` disables automatic key-result
viewers. Standalone Enter keeps its streaming terminal handoff through
`cmd.run`; that verb, `cmd.edit` and `cmd.edit-stdin` publish `foreground: true`
for hosts. `cmd.get` and `job.get` keep the structured answers (the script is the computed
prop `script`, a job's labelled streams the computed prop `output`), while
`job.show-output` returns labelled text. `cmd.show` and `job.show` open standard
record forms in the UI. See [Family integration](docs/INTEGRATION.md).
Hosted `g i` uses `cmd.run-file` with a required stdin path, so the host opens
its shared file picker before showing the captured run. Standalone `g i`
keeps its existing stdin picker and streaming execution. `cmd.rerun` (`g r`)
and `job.rerun` declare foreground execution, preserving original args, cwd
and stdin while allowing interactive reruns.

`cmd.run` (with `show`) and `cmd.run-file` publish a shared per-row `run` slot and a
15-minute captured limit. Pressing Enter twice while a run is active starts
only one run. The host can cancel it through `ochazuke.job.cancel`; a limit or
cancellation stops the process group and lets the recorder close its record.
The captured answer returns `{"title":"NAME · exit N","text":"…"}`.
The host uses that title unless `text-view.result-title=false`.

Foreground failures retain the last stderr line and, by default, wait for a
key before resuming. The host's `foreground.hold=off` disables that wait.
Leaving an edited stdin buffer empty is a plain cancellation and runs no
script. `view.jobs=false` and `view.stdin=false` publish disabled catalog verbs
for their corresponding UI actions. Refresh the host catalog after changing
these settings.


The script itself is an ordinary editable file. `edit` checkpoints it before
opening `$VISUAL`, else `$EDITOR` (default `vi`); the selected editor controls
its own writing.
Metadata changes commit immediately. `undo` restores the last rename, delete,
archive, restore, edit checkpoint, pin, dismiss or reset change, including saved script bytes;
a change over a `V` selection is one undo step. It does not
undo execution. If a restored name is occupied, restoration uses a suffix.

Storage is `$XDG_DATA_HOME/o-cmds`, default `~/.local/share/o-cmds`.
`--data DIR` overrides it; put global flags before the command. Each command has
a `<created-ts>-<slug>-<random-id>.sh` and JSON sidecar with name, description,
created time, author, optional cwd, tags and pin status. Script and metadata
permissions are `0600`; newly created storage/history directories are `0700`.
A file lock coordinates concurrent writers. `.history` holds undo snapshots.
Snapshots retain the acting framework identity in `by`, including model,
harness, pane and session; undo also records `undone_by`. The original command
author remains unchanged.
Deletion is a metadata flag; scripts are retained. An interrupted add that
leaves only a script is still listed as unassociated. Failed atomic writes
retain hidden `.pending-*` recovery files.

Author names default to `owner`. Agent additions use `O_CMDS_AUTHOR`,
`MTM_AGENT_NAME` (else `TUIOS_AGENT_NAME`) or `AGENT_NAME`; harness and pane/session fields come from
`MTM_HARNESS`/`AGENT_HARNESS` (else `TUIOS_HARNESS`), Codex/Claude markers and the `MTM_` (else `TUIOS_`) pane identifiers.
`--author` overrides the name. No environment file or credentials are read.

The TUI uses binding rows, with hints generated from the effective bindings.
Default actions are Enter run, `g i` choose stdin file, `g e` edit stdin,
`g r` rerun latest, `v` view inside the TUI, `y`
copy, `e` edit, `d d` delete, `u` undo, `s` star, `x` dismiss, `X` undismiss,
`R` reset the run count. `/` filters using ochazuke's
list/search bindings; `?` shows help; `q` quits standalone. Rebind or unbind
these through ochazuke's `bindings`/`config` verbs. No keys are switched on in
application code.

The right pane shows a compact name/description/author/age/cwd/tags header,
then the script with line numbers. Indentation, spaces and blank lines remain
visible; long lines wrap without removing spaces, and tabs display as four
spaces. The stored script and copy/pick output keep their original bytes.
`ctrl+d` / `ctrl+u` scroll the script eight display lines while the metadata
stays put. Moving the list cursor updates the preview.

`v` opens a large read-only modal without leaving the TUI. Scroll with `j` /
`k`, `ctrl+d` / `ctrl+u`, or `g` / `G` for the beginning/end; `q` or Escape
returns to the list (also in pick mode). Scripts use plain terminal monospace.
The `view.detail=false` setting hides the right pane; the viewer still works.

Hosts that mount the Commands page (mtm, nabe) draw the catalog's built-in
detail sections instead: the metadata, then the script as a fenced Markdown
code block (language from the shebang or file extension, default `sh`), with
blank lines and indentation kept. `view.detail-style=markdown` shows that look
in the standalone TUI too; the default `script` keeps the numbered view. A
host with its own `detail.sections=false` shows the plain record fields.

`o` cycles the existing `list.order` setting between oldest-first and
newest-first, keeping the cursor on the same command. This uses ochazuke's
setting-cycle verb and applies to the current session; persist your preferred
default through the settings command. Default order is oldest-first, with the
newest command at the bottom.

Optional shell convenience: `o cmds pick` opens the same Commands page, with
Enter choosing the script and exiting instead of running it. Escape cancels
with non-zero status and no stdout. The UI renders on stderr, so stdout holds
only the selected script. `o cmds pick NAME|NUMBER` prints that script directly,
using the same name/number resolver. Picking does not execute the script.

To evaluate selected commands in your current Zsh session, add this optional
line to `~/.zshrc` yourself:

```sh
eval "$(o cmds shell-init zsh)"
```

For Bash, use `eval "$(o cmds shell-init bash)"` in your shell startup file.
This defines `oc`; `--name` changes the function name:

```sh
oc                          # pick, record in history, and evaluate here
oc hello                    # evaluate a named command directly
oc 2                        # evaluate a numbered command directly
eval "$(o cmds shell-init zsh --name choose_command)"
choose_command hello        # use a different function name
```

The generated function calls `o cmds pick`, records the chosen script in shell
history, and evaluates it in the current shell, so `cd` and `export` persist.
Cancellation returns the picker's status without evaluating or adding a history
entry. Evaluation returns the script's status. The function preserves script
text and trailing newlines; the shell's normal history settings control saving
history to disk. It evaluates in the current directory; the stored `cwd`
metadata applies to `cmd run`. Use a function name containing letters, digits
and underscores, starting with a letter or underscore.

Shell initialization is entirely optional. `o-cmds`, `cmd run`, the standalone
TUI and MCP work without it. `shell-init` only prints the function; it edits no
dotfiles, installs no hooks, and needs `o-cmds` on PATH when the function runs.

```sh
o cmds setting set list.order newest-first --scope file --yes
o cmds setting set list.start-newest false --scope file --yes
o cmds setting set view.detail false --scope file --yes
o cmds setting set view.detail-style markdown --scope file --yes
o cmds setting set list.refresh 0s --scope file --yes
```

Defaults: `list.order=oldest-first`, `list.start-newest=true`,
`view.detail=true`, `view.detail-style=script`, `list.refresh=2s`. Zero refresh disables standalone polling
for commands left by agents. Existing selection is preserved on reload.
Settings use `$XDG_CONFIG_HOME/o-cmds/settings.json`; framework state uses
`$XDG_STATE_HOME/o-cmds`. Restart the page after changing detail visibility or
startup selection. Host page layout, refresh and initial selection are owned
by the host; see [integration limits](docs/INTEGRATION.md).

MCP lets agents leave commands for the owner:

```json
{
  "mcpServers": {
    "o-cmds": {"command": "/path/to/o-cmds", "args": ["mcp"]}
  }
}
```

The legacy stdio server exposes `cmd_add`, `cmd_list`, `cmd_show`, `cmd_dismiss`,
`cmd_undismiss`, `job_list` and `job_show`. It keeps the old viewer results for
`cmd_show` and `job_show`, independent of the standard `show` verbs.
`cmd_dismiss`/`cmd_undismiss` take `target`. This curated server has no execution
or deletion tools. The generic family MCP gateway discovers the full catalog.
`cmd_add` requires `script` and accepts an empty script. It also accepts
`name`, `desc`, `cwd`, `tags` and `author`. `cmd_list` takes optional `filter`;
`cmd_show` takes `target`, a name, number or stable ID. `job_list` takes optional
`command`; `job_show` requires `job`, a number, ID or unique prefix.
MCP additions also stamp the initializing client's name when agent environment
metadata is unavailable. Notifications receive no response; tool failures use
MCP error content. Stdio uses newline-delimited JSON-RPC with initialize,
ping, tools/list and tools/call.

Compared with Vim: `/` searches/filters and `d d` deletes the current item;
`u` restores a change. Script view, execution and copy operate on whole command
records. The detailed [Vim comparison](docs/VIM.md) records differences.

Checks: `make check` runs gofmt validation, vet and the new app's Go tests;
`make test` runs tests; `make build` builds locally. Builds set `GOWORK=off`,
`GOFLAGS='-trimpath -p=2'` and `GOMAXPROCS=3`, under `nice -n 15`. Test temporary
HOME/XDG paths are isolated; `TEST_TMP` defaults to `/tmp/o-cmds-lane/tests`.

Licensed under the [MIT License](LICENSE).
