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
| `v` | `v` | Different: views a script with the configured pager; does not enter Vim's visual selection mode. |
| `:edit` | `:edit` | Similar: opens the script in `$EDITOR`; editor controls its own writes. |

The app does not redefine framework spill/focus behavior, or install function
key bindings. Delete and metadata changes take effect immediately; undo
replaces discard.
