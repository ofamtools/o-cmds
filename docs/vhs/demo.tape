# Run from the repository root. Setup is hidden; visible runtime is about 13s.
Output assets/demo.gif

Require go

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 1100
Set Height 560
Set Padding 16
Set WindowBar Colorful
Set Framerate 15
Set TypingSpeed 35ms
Set CursorBlink false

Hide
Type "mkdir -p /tmp/o-cmds-brand && export O_CMDS_DEMO=$(mktemp -d /tmp/o-cmds-brand/demo.XXXXXX) && GOTMPDIR=$O_CMDS_DEMO GOWORK=off GOFLAGS='-trimpath -p=2' GOMAXPROCS=3 nice -n 15 go build -o $O_CMDS_DEMO/o-cmds-bin . && source docs/vhs/seed.sh $O_CMDS_DEMO && unset PROMPT_COMMAND && export PS1='$ ' && clear"
Enter
Wait+Line@120s /\$ $/
Show

# Creation order: row 5 is the newest, at the bottom.
Type "o-cmds cmd list"
Enter
Sleep 1.8s
Type "o-cmds cmd run 3"
Enter
Sleep 1.4s
Type "o-cmds"
Enter
Sleep 1.6s

# Start on newest, move up through the live preview, then back down.
Type "k"
Sleep 800ms
Type "j"
Sleep 800ms
Type "v"
Sleep 1.5s
Type "q"
Sleep 600ms

# Run hello; its one-second pause leaves output visible before the page resumes.
Enter
Sleep 1.6s
Type "q"
Sleep 1s

Hide
Ctrl+D
