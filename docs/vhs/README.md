# README demo

`demo.tape` records `assets/demo.gif`, the animation under the README header.
It uses Catppuccin Mocha and shows about 13 seconds: list five commands (newest
at the bottom), run number 3, then move through the TUI's script preview, open
the viewer with `v`, close it with `q`, run the newest command with Enter and
quit with `q`.

From the repository root, with [VHS](https://github.com/charmbracelet/vhs) and
the Go version in `go.mod` available:

```sh
vhs docs/vhs/demo.tape
```

The hidden setup builds the current checkout under `nice`, with `GOWORK=off`
and `GOFLAGS='-trimpath -p=2'`. It sources `seed.sh` into Bash to create a fresh
`/tmp/o-cmds-brand/demo.XXXXXX` fixture. That script isolates HOME and XDG
config/state/data directories, clears agent/session metadata, and adds a temporary
`o-cmds` wrapper to PATH that always supplies `--data` for the fixture. No
installed binary, owner database, shell startup file or config is changed.

The five scripts inspect a tiny invented project: list its files, read release
notes, check two shell scripts with `sh -n`, count lines, and print a greeting
with a one-second pause so its output stays visible before the TUI resumes.
All run in the fixture's project directory. The seed refuses an existing data
directory so another run cannot silently append duplicates. The fixture stays
in `/tmp` for inspection; each recording creates a new one.

The tape has been prepared and its CLI/TUI steps checked without running VHS.
The coordinator records and reviews the GIF before publishing the README;
`assets/demo.gif` is intentionally pending that recording.

To adapt the family demo, keep `Output assets/demo.gif` and the isolated setup,
then replace the seed commands and key sequence. Keep the window small and the
visible recording short; VHS drops unchanged frames.
