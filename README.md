<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img alt="o-cmds" src="assets/logo-light.svg" width="360">
  </picture>
  <br>
  <a href="https://github.com/unixsuperhero/o-cmds/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/unixsuperhero/o-cmds"></a>
  <a href="https://pkg.go.dev/github.com/unixsuperhero/o-cmds"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/unixsuperhero/o-cmds.svg"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/unixsuperhero/o-cmds"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/unixsuperhero/o-cmds"></a>
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
go install github.com/unixsuperhero/o-cmds@latest
```

With the installed binary on PATH:

```sh
printf 'printf "hello %%s\\n" "$1"\n' | o-cmds cmd add --name hello --desc 'Say hello'
printf 'pwd\n' | o-cmds cmd add --name where --desc 'Show the current directory'
o-cmds cmd list
o-cmds cmd show hello
o-cmds cmd run hello -- 'Ada Lovelace'
o-cmds cmd run 2                 # names or displayed numbers: NAME|N
o-cmds                           # standalone TUI, selected on newest
o-cmds pick hello                # print a script without running it
o-cmds pick                      # choose a script in the TUI
o-cmds shell-init zsh            # print an optional current-shell helper
o-cmds mcp                       # serve add/list/show over stdio JSON-RPC
```

Use `cmd add --file ./some-script.sh --name inspect --cwd /path/to/project`
to record a file and its working directory. `o-cmds catalog` prints the JSON
catalog for mtm/nabe; `o-cmds view --smoke` renders one frame without a terminal.
The [demo tape](docs/vhs/README.md) records the header animation.

`cmd list` prints numbered columns: name, description, age, author. Numbers are
positions in the current order; filtering preserves the unfiltered numbers.
Names take precedence over numeric positions. Stable `cmd/ID` targets from JSON
rows remain usable across renames and changes to the list.

Every command-targeting verb and MCP `cmd_show` also accepts a unique name
prefix: `cmd show play` can select `play-o-json-tutorials`. Matching reuses
oden's resolver, including its descendant rule: a prefix matching both `play`
and `play-tutorials` selects `play`. Exact names and valid numbered positions
take precedence. Ambiguous input prints the matching commands with their
current list numbers and returns a failure; it never runs or changes a command.

The pinned oden v0.1.1 supports exact/prefix matching only. Dash-segment forms
(`p-o-j-t`, `pojt`) and last-resort word fragments require a shared oden matcher
extension; they are not implemented by a separate app matcher.

```sh
o-cmds cmd show 1
o-cmds cmd copy hello            # print the exact script bytes
o-cmds cmd copy hello --yank     # also copy to the clipboard
EDITOR='code --wait' o-cmds cmd edit hello
o-cmds cmd rename hello greeting
o-cmds cmd pin greeting          # show a star, keep chronological position
o-cmds cmd star greeting         # toggle the pin
o-cmds cmd unpin greeting
o-cmds cmd delete greeting       # hide; keep script and history
o-cmds cmd undo                  # restore the most recent change
o-cmds cmd path greeting         # script path
o-cmds cmd path                  # storage directory
o-cmds cmd list --filter deploy
o-cmds cmd list --json '{}'      # catalog rows, including script/metadata
o-cmds cmd list --format table   # framework table output
o-cmds cmd add --name report --script 'pwd' --tags '["work","daily"]'
```

Runs use `sh SCRIPT ARG...`, with the recorded cwd if present. Otherwise they
inherit the caller's directory. Stdin, stdout and stderr stream directly, and
the CLI returns the script's exit status. A missing recorded directory does
not prevent saving; running reports the directory error. Script bytes are
preserved, including when the script is empty or has no trailing newline.

The script itself is an ordinary editable file. `edit` checkpoints it before
opening `$EDITOR` (default `vi`); the selected editor controls its own writing.
Metadata changes commit immediately. `undo` restores the last rename, delete,
edit checkpoint, or pin change, including the saved script bytes. It does not
undo execution. If a restored name is occupied, restoration uses a suffix.

Storage is `$XDG_DATA_HOME/o-cmds`, default `~/.local/share/o-cmds`.
`--data DIR` overrides it; put global flags before the command. Each command has
a `<created-ts>-<slug>-<random-id>.sh` and JSON sidecar with name, description,
created time, author, optional cwd, tags and pin status. Script and metadata
permissions are `0600`; newly created storage/history directories are `0700`.
A file lock coordinates concurrent writers. `.history` holds undo snapshots.
Deletion is a metadata flag; scripts are retained. An interrupted add that
leaves only a script is still listed as unassociated. Failed atomic writes
retain hidden `.pending-*` recovery files.

Author names default to `owner`. Agent additions use `O_CMDS_AUTHOR`,
`TUIOS_AGENT_NAME` or `AGENT_NAME`; harness and pane/session fields come from
`TUIOS_HARNESS`/`AGENT_HARNESS`, Codex/Claude markers and TUIOS identifiers.
`--author` overrides the name. No environment file or credentials are read.

The TUI uses binding rows, with hints generated from the effective bindings.
Default actions are Enter run, `v` view inside the TUI, `y`
copy, `e` edit, `d d` delete, `u` undo, `s` star. `/` filters using ochazuke's
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

`o` cycles the existing `list.order` setting between oldest-first and
newest-first, keeping the cursor on the same command. This uses ochazuke's
setting-cycle verb and applies to the current session; persist your preferred
default through the settings command. Default order is oldest-first, with the
newest command at the bottom.

Optional shell convenience: `o-cmds pick` opens the same Commands page, with
Enter choosing the script and exiting instead of running it. Escape cancels
with non-zero status and no stdout. The UI renders on stderr, so stdout holds
only the selected script. `o-cmds pick NAME|NUMBER` prints that script directly,
using the same name/number resolver. Picking does not execute the script.

To evaluate selected commands in your current Zsh session, add this optional
line to `~/.zshrc` yourself:

```sh
eval "$(o-cmds shell-init zsh)"
```

For Bash, use `eval "$(o-cmds shell-init bash)"` in your shell startup file.
This defines `oc`; `--name` changes the function name:

```sh
oc                          # pick, record in history, and evaluate here
oc hello                    # evaluate a named command directly
oc 2                        # evaluate a numbered command directly
eval "$(o-cmds shell-init zsh --name choose_command)"
choose_command hello        # use a different function name
```

The generated function calls `o-cmds pick`, records the chosen script in shell
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
o-cmds settings set list.order newest-first --yes
o-cmds settings set list.start-newest false --yes
o-cmds settings set view.detail false --yes
o-cmds settings set list.refresh 0s --yes
```

Defaults: `list.order=oldest-first`, `list.start-newest=true`,
`view.detail=true`, `list.refresh=2s`. Zero refresh disables standalone polling
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

The stdio server exposes exactly `cmd_add`, `cmd_list`, `cmd_show`. It has **no
`cmd_run` tool**. `cmd_add` requires `script` (empty is valid), and accepts
`name`, `desc`, `cwd`, `tags` and `author`. `cmd_list` takes optional `filter`;
`cmd_show` takes `target` (a name, number as a string, or stable ID).
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
