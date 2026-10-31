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

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/keytest"
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
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var story struct {
				Name     string
				Commands []Add
				Picker   bool
				Settings map[string]any
				Script   []string
				Expect   []struct {
					After                            int
					Contains, Lacks                  []string
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
			if e = json.Unmarshal(data, &story); e != nil {
				t.Fatal(e)
			}
			s := fixture(t)
			for _, c := range story.Commands {
				if _, e = s.Add(c); e != nil {
					t.Fatal(e)
				}
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
			p.m.Foreground = func(ochazuke.ForegroundMsg) tea.Cmd { t.Fatal("viewer attempted a foreground process"); return nil }
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
				p.Update(keytest.Msg(key, false, false))
				check(i + 1)
			}
		})
	}
}
