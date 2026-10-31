#!/bin/sh
# Source from Bash after building ROOT/o-cmds-bin. All state stays in ROOT.
# Usage: source docs/vhs/seed.sh /tmp/o-cmds-brand/demo.XXXXXX
set -eu
if [ "$#" -ne 1 ]; then
    printf '%s\n' 'usage: source docs/vhs/seed.sh /tmp/o-cmds-brand/demo.XXXXXX' >&2
    return 2
fi
case "$1" in
    /tmp/o-cmds-brand/*) ;;
    *) printf '%s\n' 'demo root must be under /tmp/o-cmds-brand/' >&2; return 2 ;;
esac
if [ ! -x "$1/o-cmds-bin" ] || [ -e "$1/data" ]; then
    printf '%s\n' 'build ROOT/o-cmds-bin first and use a fresh demo root' >&2
    return 2
fi

export O_CMDS_DEMO="$1"
export HOME="$O_CMDS_DEMO/home"
export XDG_CONFIG_HOME="$O_CMDS_DEMO/config"
export XDG_STATE_HOME="$O_CMDS_DEMO/state"
export XDG_DATA_HOME="$O_CMDS_DEMO/share"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME" \
    "$O_CMDS_DEMO/bin" "$O_CMDS_DEMO/project/scripts" "$O_CMDS_DEMO/project/notes"

# Keep the owner's agent/session metadata out of the fixture and recording.
unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_AGENT_NAME TUIOS_HARNESS \
    AGENT_NAME AGENT_HARNESS CODEX_THREAD_ID CODEX_CI CLAUDE_CODE_ENTRYPOINT
export O_CMDS_AUTHOR=agent
cat > "$O_CMDS_DEMO/bin/o-cmds" <<'SH'
#!/bin/sh
exec "$O_CMDS_DEMO/o-cmds-bin" --data "$O_CMDS_DEMO/data" "$@"
SH
chmod +x "$O_CMDS_DEMO/bin/o-cmds"
export PATH="$O_CMDS_DEMO/bin:$PATH"

cat > "$O_CMDS_DEMO/project/scripts/hello.sh" <<'SH'
#!/bin/sh
printf '%s\n' 'Ready to run.' 'Newest commands are at the bottom.'
SH
cat > "$O_CMDS_DEMO/project/scripts/check.sh" <<'SH'
#!/bin/sh
set -eu
for script in scripts/*.sh; do
    sh -n "$script"
    printf 'OK %s\n' "$script"
done
SH
cat > "$O_CMDS_DEMO/project/notes/release.md" <<'MD'
# Release checklist

- Check shell syntax.
- Review the README demo.
- Let the owner choose when to run.
MD

o-cmds cmd add --name project-files --desc 'List the project files' \
    --cwd "$O_CMDS_DEMO/project" --script 'find . -type f | sort' > /dev/null
o-cmds cmd add --name release-notes --desc 'Read the release checklist' \
    --cwd "$O_CMDS_DEMO/project" --script 'cat notes/release.md' > /dev/null
o-cmds cmd add --name check-scripts --desc 'Check shell syntax' \
    --cwd "$O_CMDS_DEMO/project" --script 'sh scripts/check.sh' > /dev/null
o-cmds cmd add --name count-lines --desc 'Count script and note lines' \
    --cwd "$O_CMDS_DEMO/project" --script 'wc -l scripts/*.sh notes/*.md' > /dev/null
o-cmds cmd add --name hello --desc 'Run the newest command' \
    --cwd "$O_CMDS_DEMO/project" --script '# Run the project greeting.
set -eu

sh scripts/hello.sh
sleep 1 # Keep the output visible before returning to the TUI.' > /dev/null

cd "$O_CMDS_DEMO/project"
