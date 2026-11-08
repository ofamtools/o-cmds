package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

// App stories exercise the real registered page and binding rows. The shared
// o-story fixture runner cannot construct an o-cmds app; this adapter can.
func TestScriptStories(t *testing.T) {
	files, e := filepath.Glob("cmd/o-story/stories/cmds/script-*.json")
	if e != nil {
		t.Fatal(e)
	}
	if len(files) == 0 {
		t.Fatal("no script stories")
	}
	runFiles, e := filepath.Glob("cmd/o-story/stories/cmds/run-*.json")
	if e != nil {
		t.Fatal(e)
	}
	files = append(files, runFiles...)
	historyFiles, e := filepath.Glob("cmd/o-story/stories/cmds/history-*.json")
	if e != nil {
		t.Fatal(e)
	}
	files = append(files, historyFiles...)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var story struct {
				Name       string
				Commands   []Add
				Picker     bool
				Foreground bool
				Env        map[string]string
				Inputs     map[string]string
				Settings   map[string]any
				Runs       []struct {
					Command, Stdout, Stderr string
					ExitCode                int
				}
				Script []string
				Expect []struct {
					After                            int
					Contains, Lacks                  []string
					RunInput, RunKind                string
					RunCount                         *int
					Status                           map[string]string
					Toned, Plain                     []string
					Selected                         string
					Order                            []string
					Modal                            *bool
					OffsetMin                        *int
					ViewerOffsetMin, ViewerOffsetMax *int
				}
			}
			data, e := os.ReadFile(file)
			if e != nil {
				t.Fatal(e)
			}
			// A skipped story records a known gap and may use the older
			// fixture shape, so skip before decoding the rest.
			var gap struct{ Skip string }
			if e = json.Unmarshal(data, &gap); e != nil {
				t.Fatal(e)
			}
			if gap.Skip != "" {
				t.Skip(gap.Skip)
			}
			if e = json.Unmarshal(data, &story); e != nil {
				t.Fatal(e)
			}
			s := fixture(t)
			for key, value := range story.Env {
				t.Setenv(key, value)
			}
			if len(story.Inputs) > 0 {
				dir := filepath.Join(s.Dir, "inputs")
				if e := os.MkdirAll(dir, 0o700); e != nil {
					t.Fatal(e)
				}
				for name, text := range story.Inputs {
					if filepath.Base(name) != name {
						t.Fatal("invalid story input name")
					}
					if e := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); e != nil {
						t.Fatal(e)
					}
				}
				t.Chdir(dir)
			}
			for _, c := range story.Commands {
				if _, e = s.Add(c); e != nil {
					t.Fatal(e)
				}
			}
			for i, seed := range story.Runs {
				c, e := s.Resolve(seed.Command, false)
				if e != nil {
					t.Fatal(e)
				}
				at := s.now().Add(time.Duration(i) * time.Second)
				r := Run{ID: fmt.Sprintf("story-run-%d", i+1), CommandID: c.ID, CommandName: c.Name, Started: at, Ended: &at, ExitCode: &seed.ExitCode, StdoutBytes: int64(len(seed.Stdout)), StderrBytes: int64(len(seed.Stderr)), StdoutKept: len(seed.Stdout), StderrKept: len(seed.Stderr)}
				if e := s.locked(func() error { return s.writeRun(r, []byte(seed.Stdout), []byte(seed.Stderr)) }); e != nil {
					t.Fatal(e)
				}
			}
			// Settings read at start (view.job-columns) come from the
			// settings file, as they do for the owner.
			if len(story.Settings) > 0 {
				writeSettings(t, story.Settings)
			}
			var out bytes.Buffer
			a, e := newApp(s, strings.NewReader(""), &out, &out)
			if e != nil {
				t.Fatal(e)
			}
			for name, value := range story.Settings {
				if e = a.r.Settings().Set(name, value); e != nil {
					t.Fatal(e)
				}
			}
			var p *page
			if story.Picker {
				var pick *picker
				pick, e = mountPicker(a)
				if e == nil {
					p = pick.page
				}
			} else {
				p, e = mountPage(a)
			}
			if e != nil {
				t.Fatal(e)
			}
			p.m.Width, p.m.Height = 120, 26
			p.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd {
				if !story.Foreground {
					t.Fatal("viewer attempted a foreground process")
				}
				return func() tea.Msg { return msg.Done(a.run(msg.Cmd)) }
			}
			check := func(after int) {
				frame := ansi.Strip(p.m.View().Content)
				for _, want := range story.Expect {
					if want.After != after {
						continue
					}
					for _, text := range want.Contains {
						if !strings.Contains(frame, text) {
							t.Fatalf("%s after %d missing %q:\n%s", story.Name, after, text, frame)
						}
					}
					for _, text := range want.Lacks {
						if strings.Contains(frame, text) {
							t.Fatalf("after %d unexpectedly contains %q:\n%s", after, text, frame)
						}
					}
					if want.RunCount != nil {
						runs, e := s.Runs("")
						if e != nil || len(runs) != *want.RunCount {
							t.Fatalf("after %d runs=%v err=%v", after, runs, e)
						}
						if len(runs) > 0 && want.RunKind != "" {
							r, e := s.ReadRun(runs[len(runs)-1].ID)
							if e != nil || r.Stdin.Kind != want.RunKind || r.Input != want.RunInput || r.Stdout != want.RunInput {
								t.Fatalf("after %d stdin run=%+v err=%v", after, r, e)
							}
						}
					}
					items := map[string]ochazuke.ListItem{}
					for _, item := range p.v.List(listID).Items() {
						items[item.Label] = item
					}
					for label, status := range want.Status {
						if got := items[label].Props["status"]; got != status {
							t.Fatalf("after %d %s status=%v want %s", after, label, got, status)
						}
					}
					for _, label := range want.Toned {
						if len(items[label].Tones) == 0 {
							t.Fatalf("after %d %s is not toned", after, label)
						}
					}
					for _, label := range want.Plain {
						if len(items[label].Tones) != 0 {
							t.Fatalf("after %d %s is toned: %v", after, label, items[label].Tones)
						}
					}
					if want.Selected != "" {
						selected, ok := p.v.List(listID).Selected()
						if !ok || selected.Label != want.Selected {
							t.Fatalf("after %d selected=%+v", after, selected)
						}
					}
					if want.Order != nil {
						var order []string
						for _, item := range p.v.List(listID).Items() {
							order = append(order, item.Label)
						}
						if !reflect.DeepEqual(order, want.Order) {
							t.Fatalf("after %d order=%v want=%v", after, order, want.Order)
						}
					}
					if want.Modal != nil && (a.r.TopModal() != "") != *want.Modal {
						t.Fatalf("after %d modal=%q", after, a.r.TopModal())
					}
					if want.OffsetMin != nil {
						d := a.r.Detail(pageID + "/detail")
						if d == nil || d.Offset() < *want.OffsetMin {
							t.Fatalf("after %d detail did not scroll", after)
						}
					}
					if want.ViewerOffsetMin != nil || want.ViewerOffsetMax != nil {
						d := a.r.Detail(scriptViewerTextID)
						if d == nil {
							t.Fatalf("after %d has no viewer detail", after)
						}
						if want.ViewerOffsetMin != nil && d.Offset() < *want.ViewerOffsetMin {
							t.Fatalf("after %d viewer offset=%d", after, d.Offset())
						}
						if want.ViewerOffsetMax != nil && d.Offset() > *want.ViewerOffsetMax {
							t.Fatalf("after %d viewer offset=%d", after, d.Offset())
						}
					}
				}
				if a.r.Quit {
					t.Fatalf("story unexpectedly quit after %d", after)
				}
				if dir := os.Getenv("O_CMDS_STORY_ARTIFACTS"); dir != "" {
					if e := os.MkdirAll(dir, 0o700); e != nil {
						t.Fatal(e)
					}
					if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%02d.txt", filepath.Base(file), after)), []byte(frame), 0o600); e != nil {
						t.Fatal(e)
					}
				}
			}
			check(0)
			for i, key := range story.Script {
				_, cmd := p.Update(keytest.Msg(key, false, false))
				if cmd != nil {
					deliver(t, p, cmd())
				}
				check(i + 1)
			}
		})
	}
}

func writeSettings(t *testing.T, settings map[string]any) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "o-cmds")
	if e := os.MkdirAll(dir, 0o700); e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(settings)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "settings.json"), b, 0o600); e != nil {
		t.Fatal(e)
	}
}
