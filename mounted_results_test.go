package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	for _, name := range []string{"host-script-viewer", "host-foreground"} {
		t.Run(name, func(t *testing.T) {
			var story struct {
				Commands []Add
				Script   []string
				Expect   []struct {
					After    int
					Contains []string
					Modal    bool
				}
			}
			data, err := os.ReadFile(filepath.Join("cmd/o-story/stories/cmds", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &story); err != nil {
				t.Fatal(err)
			}
			s := fixture(t)
			for _, c := range story.Commands {
				if _, err := s.Add(c); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			a, err := newApp(s, strings.NewReader(""), &out, &out)
			if err != nil {
				t.Fatal(err)
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
				if err := ocli.Settle(r, nil); err != nil {
					t.Fatal(err)
				}
				for _, want := range story.Expect {
					if want.After != i+1 {
						continue
					}
					if (r.TopModal() != "") != want.Modal {
						t.Fatalf("after %d modal=%q", i+1, r.TopModal())
					}
					for _, text := range want.Contains {
						in := r.Input(r.Focus)
						if in == nil || !strings.Contains(string(in.Text), text) {
							t.Fatalf("after %d missing %q: %s", i+1, text, m.View().Content)
						}
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
	var text string
	if err := json.Unmarshal(out.Bytes(), &text); err != nil {
		t.Fatal(err)
	}
	if text != "failed · exit 7\n\n--- stdout ---\nout proof\n--- stderr ---\nerr proof" {
		t.Fatalf("captured failure = %q", text)
	}
	if runs, err := s.Runs(""); err != nil || len(runs) != 0 {
		t.Fatalf("retained runs = %v, %v", runs, err)
	}
}
