package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// These stories cross a real captured-process boundary, unlike the standalone
// page fixture. The executable and every record/config live in temp dirs.
func TestMountedResultStories(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "o-cmds")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	files, err := filepath.Glob("cmd/o-story/stories/cmds/host-*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(name, func(t *testing.T) {
			var story struct {
				Commands []Add
				Inputs   map[string]string
				Settings map[string]any
				Script   []string
				Editor   string
				Timeout  string
				NoWait   []int
				RunCount *int
				Expect   []struct {
					After    int
					Contains []string
					Lacks    []string
					Modal    bool
					Error    string
				}
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var gap struct{ Skip string }
			if err := json.Unmarshal(data, &gap); err != nil {
				t.Fatal(err)
			}
			if gap.Skip != "" {
				t.Skip(gap.Skip)
			}
			if err := json.Unmarshal(data, &story); err != nil {
				t.Fatal(err)
			}
			s := fixture(t)
			if story.Editor != "" {
				t.Setenv("EDITOR", story.Editor)
				t.Setenv("VISUAL", story.Editor)
			}
			for _, c := range story.Commands {
				if _, err := s.Add(c); err != nil {
					t.Fatal(err)
				}
			}
			if len(story.Inputs) > 0 {
				dir := filepath.Join(s.Dir, "inputs")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				for name, text := range story.Inputs {
					if filepath.Base(name) != name {
						t.Fatal("invalid input fixture name")
					}
					if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				t.Chdir(dir)
			}
			var out bytes.Buffer
			a, err := newApp(s, strings.NewReader(""), &out, &out)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range story.Settings {
				if err := a.r.Settings().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := ocli.Catalog(a.r, "o-cmds", version)
			if err != nil {
				t.Fatal(err)
			}
			var doc ocli.CatalogDoc
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			r := ochazuke.NewRuntime()
			r.Config().Disable()
			m := ochazuke.NewModel(r)
			m.Width, m.Height = 120, 26
			mounted := ocli.MountNouns(r, doc.AppNouns(), ocli.HostMount{Local: func(n string) string { return "host." + n }, Proxy: func(noun string, v ocli.CatalogVerb) any {
				if story.Timeout != "" {
					v.Timeout = story.Timeout
				}
				return func(c ochazuke.Ctx) ochazuke.Result {
					args := []byte("{}")
					if len(c.Args) > 0 {
						args, _ = json.Marshal(c.Args)
					}
					target, _ := c.Instance.Data.(string)
					return ocli.RunMounted(v, ocli.ProxyCommand{Bin: bin, Args: []string{"--data", s.Dir, noun, v.Name, "--target", target, "--json", string(args), "--yes"}})
				}
			}})
			if len(mounted.Warnings) > 0 {
				t.Fatal(mounted.Warnings)
			}
			for _, row := range doc.Bindings {
				if local, ok := mounted.Nouns[row.Noun]; ok {
					row.Noun = local
					row.Request.Noun = mounted.Nouns[row.Request.Noun]
					row.When = "" // Published rows defer only their remote guards.
					if row.Request.Noun != "" {
						if err := r.Bind(row); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			v, err := ocli.MountPage(m, doc, commandPage(true), ocli.PageConfig{Noun: func(n string) string { return mounted.Nouns[n] }})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := a.rows("")
			if err != nil {
				t.Fatal(err)
			}
			if err := v.SetRows(listID, rows); err != nil {
				t.Fatal(err)
			}
			if err := r.OpenPage(pageID); err != nil {
				t.Fatal(err)
			}
			if err := v.List(listID).Focus(); err != nil {
				t.Fatal(err)
			}
			_ = m.View()
			for i, key := range story.Script {
				if _, err := r.Dispatch(ochazuke.MacroKey{Key: key}); err != nil {
					t.Fatal(err)
				}
				var settled error
				if !slices.Contains(story.NoWait, i+1) {
					settled = ocli.Settle(r, nil)
				}
				var wantError string
				for _, want := range story.Expect {
					if want.After == i+1 {
						wantError = want.Error
					}
				}
				if wantError == "" && settled != nil || wantError != "" && (settled == nil || !strings.Contains(settled.Error(), wantError)) {
					t.Fatalf("after %d error=%v, want %q", i+1, settled, wantError)
				}
				frame := ansi.Strip(m.View().Content)
				if dir := os.Getenv("O_CMDS_STORY_ARTIFACTS"); dir != "" {
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.txt", name, i+1)), []byte(frame), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for _, want := range story.Expect {
					if want.After != i+1 {
						continue
					}
					if (r.TopModal() != "") != want.Modal {
						t.Fatalf("after %d modal=%q", i+1, r.TopModal())
					}
					for _, text := range want.Contains {
						if !strings.Contains(frame, text) {
							t.Fatalf("after %d missing %q: %s", i+1, text, frame)
						}
					}
					for _, text := range want.Lacks {
						if strings.Contains(frame, text) {
							t.Fatalf("after %d unexpectedly contains %q: %s", i+1, text, frame)
						}
					}
				}
			}
			if story.RunCount != nil {
				runs, err := s.Runs("")
				if err != nil || len(runs) != *story.RunCount {
					t.Fatalf("retained runs=%v, err=%v; want %d", runs, err, *story.RunCount)
				}
				for _, run := range runs {
					if run.Ended == nil {
						t.Fatalf("run left unfinished: %+v", run)
					}
				}
			}
		})
	}
}

func TestRunViewFailureWithoutRetention(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "failed", "printf 'out proof'; printf 'err proof' >&2; exit 7")
	var out, errOut bytes.Buffer
	a, err := newApp(s, strings.NewReader(""), &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.r.Settings().Set("runs.keep", 0); err != nil {
		t.Fatal(err)
	}
	if err := a.shell().Run(context.Background(), []string{"cmd", "run-view", "failed"}); err != nil {
		t.Fatal(err)
	}
	var text ochazuke.TextAnswer
	if err := json.Unmarshal(out.Bytes(), &text); err != nil {
		t.Fatal(err)
	}
	if text.Title != "failed · exit 7" || text.Text != "failed · exit 7\n\n--- stdout ---\nout proof\n--- stderr ---\nerr proof" {
		t.Fatalf("captured failure = %q", text)
	}
	if runs, err := s.Runs(""); err != nil || len(runs) != 0 {
		t.Fatalf("retained runs = %v, %v", runs, err)
	}
}
