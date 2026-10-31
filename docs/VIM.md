# Compared with Vim

Comparison checked against the local Vim 9.1 `:help` files before implementing
the page. Keys are application binding rows; the framework owns motions.

| Vim form | `:help` tag | o-cmds |
| --- | --- | --- |
| `/pattern` | `/`, `search-pattern` | Similar: ochazuke list filtering/search, rather than moving through a text buffer. |
| `j` / `k` | `j`, `k` | Similar: visual list movement supplied by ochazuke. |
| `G` | `G` | Similar: framework list end motion; default startup selects newest without a key. |
| `d d` | `dd` | Similar: hides the current command, retaining file/history, instead of deleting a text line into a register. |
| `u` | `u`, `:undo` | Similar: restores a command snapshot, including script bytes. No undo of process side effects. |
| `y` | `y` | Similar: yanks the whole script to the clipboard, rather than starting a text operator. |
| `:!cmd` | `:!` | Similar: Enter runs the selected saved script through sh, handing off the terminal standalone. |
| `v` | `v` | Different: opens a read-only script modal; does not enter Vim's visual selection mode. |
| `j` / `k` in a buffer | `j`, `k` | Similar: scrolls the script modal one display line instead of moving a text cursor. |
| Ctrl-D / Ctrl-U | `CTRL-D`, `CTRL-U` | Similar: scrolls the preview or modal, eight display lines rather than Vim's half-window default. |
| `gg` / `G` | `gg`, `G` | Similar: modal `g` / `G` scrolls to beginning/end; beginning uses one `g`, without a text cursor. |
| `q` / Escape | `q`, `<Esc>` | Different: closes the script modal and restores the list, rather than recording a macro or leaving a Vim mode. |
| `o` | `o` | Different: cycles list.order while preserving selection, rather than inserting a new text line. |
| `:edit` | `:edit` | Similar: opens the script in `$EDITOR`; editor controls its own writes. |
| `inputlist()` | `inputlist()` | Similar: optional `pick` chooses one item or cancels; it returns script text on stdout rather than a Vim numeric result. |
| Shell function initialization | n/a | A shell convenience for history and current-shell evaluation; no Vim interaction counterpart. |
| `:history` | `:history`, `:history-indexing` | Similar: run list shows numbered history with newest at bottom. Ours records execution and separate streams, and numbers shift after deletion/retention; Vim's history indices remain fixed. |
| `inputlist()` | `inputlist()` | Similar: H opens the framework's searchable history chooser with output preview; Enter opens a read-only output viewer instead of returning a numeric answer. |
| `H` | `H` | Different: opens command run history, rather than moving to the top of the screen. No default H row existed in this app or the pinned framework catalog. |

The app does not redefine framework spill/focus behavior, or install function
key bindings. Delete and metadata changes take effect immediately; undo
replaces discard.
