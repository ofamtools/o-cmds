package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

func TestPickEnterPrintsSelectedScript(t *testing.T) {
	for _, script := range []string{"cd /tmp\nexport PICKED='two words'\n", "", "printf no-final-newline"} {
		t.Run(script, func(t *testing.T) {
			s := fixture(t)
			addCommand(t, s, "old", "old")
			addCommand(t, s, "chosen", script)
			var out, ui bytes.Buffer
			a, e := newApp(s, strings.NewReader(""), &out, &ui)
			if e != nil {
				t.Fatal(e)
			}
			a.run = func(*exec.Cmd) error { t.Fatal("pick ran a process"); return nil }
			p, e := mountPicker(a)
			if e != nil {
				t.Fatal(e)
			}
			p.page.m.Foreground = func(ochazuke.ForegroundMsg) tea.Cmd { t.Fatal("pick started a foreground command"); return nil }
			p.page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if !a.r.Quit {
				t.Fatal("Enter did not exit")
			}
			if e = p.writeSelection(&out); e != nil {
				t.Fatal(e)
			}
			if out.String() != script {
				t.Fatalf("stdout=%q want=%q", out.String(), script)
			}
		})
	}
}

func TestPickEscapePrintsNothingAndFails(t *testing.T) {
	for _, filter := range []bool{false, true} {
		t.Run(map[bool]string{false: "list", true: "filter"}[filter], func(t *testing.T) {
			s := fixture(t)
			addCommand(t, s, "sample", "echo sample")
			var out, ui bytes.Buffer
			a, e := newApp(s, strings.NewReader(""), &out, &ui)
			if e != nil {
				t.Fatal(e)
			}
			p, e := mountPicker(a)
			if e != nil {
				t.Fatal(e)
			}
			if filter {
				p.page.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
				p.page.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
			}
			p.page.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if !a.r.Quit {
				t.Fatal("Escape did not exit")
			}
			if e = p.writeSelection(&out); oden.ExitCode(e) == 0 {
				t.Fatalf("cancel error=%v", e)
			}
			if out.Len() != 0 {
				t.Fatalf("cancel stdout=%q", out.String())
			}
		})
	}
}

func TestPickArgumentForm(t *testing.T) {
	s := fixture(t)
	script := "export DIRECT_PICK='value with spaces'\n"
	addCommand(t, s, "sample-command", script)
	for _, target := range []string{"sample-command", "sample", "1"} {
		out, e := invoke(t, s, "", "pick", target)
		if e != nil || out != script {
			t.Fatalf("pick %q=%q error=%v", target, out, e)
		}
	}
	if out, e := invoke(t, s, "", "pick", "missing"); e == nil || out != "" {
		t.Fatalf("missing=%q %v", out, e)
	}
	if out, e := invoke(t, s, "", "pick", "1", "extra"); e == nil || out != "" {
		t.Fatalf("extra=%q %v", out, e)
	}
}

func TestPickDoesNotChangeNormalPageBinding(t *testing.T) {
	s := fixture(t)
	addCommand(t, s, "sample", "echo sample")
	var out bytes.Buffer
	a, e := newApp(s, strings.NewReader(""), &out, &out)
	if e != nil {
		t.Fatal(e)
	}
	p, e := mountPage(a)
	if e != nil {
		t.Fatal(e)
	}
	called := false
	p.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd { called = true; return nil }
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		deliver(t, p, cmd())
	}
	if !called {
		t.Fatal("normal Enter no longer runs")
	}
}
