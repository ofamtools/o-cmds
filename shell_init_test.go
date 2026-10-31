package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellInitOutput(t *testing.T) {
	s := fixture(t)
	for _, shell := range []string{"zsh", "bash"} {
		out, e := invoke(t, s, "", "shell-init", shell)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out, "function oc {") || !strings.Contains(out, `command o-cmds pick "$@"`) || !strings.Contains(out, `builtin eval -- "$__o_cmds_script"`) {
			t.Fatalf("%s init=%s", shell, out)
		}
		out, e = invoke(t, s, "", "shell-init", shell, "--name", "choose_command")
		if e != nil || !strings.Contains(out, "function choose_command {") {
			t.Fatalf("custom init=%q %v", out, e)
		}
	}
	for _, args := range [][]string{{"shell-init", "fish"}, {"shell-init", "zsh", "--name", "bad;name"}, {"shell-init", "bash", "--name", "2bad"}, {"shell-init", "bash", "--name", ""}} {
		out, e := invoke(t, s, "", args...)
		if e == nil || out != "" {
			t.Fatalf("invalid args=%q output=%q error=%v", args, out, e)
		}
	}
}

// Run the emitted function in real isolated shells, adapting the gmtm shell
// fixture pattern. The executable on PATH is a fake picker, not owner data.
func TestShellInitCurrentShellHistoryAndCancellation(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		for _, mode := range []string{"success", "cancel", "eval-failure"} {
			t.Run(shell+"/"+mode, func(t *testing.T) {
				fixture(t)
				binary, e := exec.LookPath(shell)
				if e != nil {
					t.Skip(shell + " not installed")
				}
				root, e := filepath.EvalSymlinks(t.TempDir())
				if e != nil {
					t.Fatal(e)
				}
				bin := filepath.Join(root, "bin")
				if e = os.Mkdir(bin, 0o700); e != nil {
					t.Fatal(e)
				}
				target := filepath.Join(root, "target directory")
				if e = os.Mkdir(target, 0o700); e != nil {
					t.Fatal(e)
				}
				fake := `#!/bin/sh
printf '%s\000' "$@" > "$OC_CALL_LOG"
if [ "$OC_PICK_MODE" = cancel ]; then
  printf 'export OC_LEAK=should-not-be-evaluated'
  exit 9
fi
cat "$OC_PICK_SCRIPT"
`
				if e = os.WriteFile(filepath.Join(bin, "o-cmds"), []byte(fake), 0o700); e != nil {
					t.Fatal(e)
				}
				script := `cd "$OC_TARGET_DIR"
export OC_SELECTION='value from selected script'
`
				if mode == "eval-failure" {
					script += "false\n"
				}
				scriptPath := filepath.Join(root, "picked.sh")
				if e = os.WriteFile(scriptPath, []byte(script), 0o600); e != nil {
					t.Fatal(e)
				}
				var init bytes.Buffer
				if e = shellInit([]string{shell, "--name", "choose_command"}, &init, &init); e != nil {
					t.Fatal(e)
				}
				initPath := filepath.Join(root, "init.sh")
				if e = os.WriteFile(initPath, init.Bytes(), 0o600); e != nil {
					t.Fatal(e)
				}
				seed, history := "builtin history -s -- seed", "builtin history 1"
				flags := []string{"--noprofile", "--norc", "-i", "-c"}
				if shell == "zsh" {
					flags = []string{"-f", "-i", "-c"}
					seed, history = "builtin print -s -- seed", "builtin fc -ln -1"
				}
				invocation := `. "$OC_INIT"
` + seed + `
choose_command --data "$OC_DATA" 'name with spaces'
oc_result=$?
printf 'RESULT=%s\nCWD=%s\nVALUE=%s\nLEAK=%s\n' "$oc_result" "$PWD" "$OC_SELECTION" "$OC_LEAK"
printf 'HISTORY\n'
` + history + "\n"
				cmd := exec.Command(binary, append(flags, invocation)...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "ZDOTDIR="+os.Getenv("HOME"), "HISTFILE="+filepath.Join(root, "history"), "HISTCONTROL=", "HISTIGNORE=", "OC_INIT="+initPath, "OC_CALL_LOG="+filepath.Join(root, "args"), "OC_PICK_SCRIPT="+scriptPath, "OC_PICK_MODE="+mode, "OC_TARGET_DIR="+target, "OC_DATA="+filepath.Join(root, "data"), "OC_SELECTION=", "OC_LEAK=")
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				out, e := cmd.Output()
				if e != nil {
					t.Fatalf("shell: %v stdout=%s stderr=%s", e, out, stderr.String())
				}
				text := string(out)
				if mode == "cancel" {
					if !strings.Contains(text, "RESULT=9\nCWD="+root+"\nVALUE=\nLEAK=\nHISTORY\n") || !strings.HasSuffix(strings.TrimSpace(text), "seed") {
						t.Fatalf("cancel=%s", text)
					}
				} else {
					code := "0"
					if mode == "eval-failure" {
						code = "1"
					}
					if !strings.Contains(text, "RESULT="+code+"\nCWD="+target+"\nVALUE=value from selected script\n") {
						t.Fatalf("eval=%s", text)
					}
					parts := strings.SplitN(text, "HISTORY\n", 2)
					if len(parts) != 2 || !strings.Contains(parts[1], "export OC_SELECTION='value from selected script'") {
						t.Fatalf("history=%s", text)
					}
				}
				args, e := os.ReadFile(filepath.Join(root, "args"))
				want := "pick\x00--data\x00" + filepath.Join(root, "data") + "\x00name with spaces\x00"
				if e != nil || string(args) != want {
					t.Fatalf("args=%q error=%v", args, e)
				}
			})
		}
	}
}
