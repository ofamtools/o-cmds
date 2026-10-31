package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

const scriptDetailKind = "script"
const scriptViewerID = "script-viewer"
const scriptViewerTextID = scriptViewerID + "/text"

// Line numbers are a display projection only. Source, blank lines and leading
// spaces remain untouched in storage and in copy/pick/run results.
func numberedScript(source string) string {
	lines := strings.Split(source, "\n")
	if strings.HasSuffix(source, "\n") {
		lines = lines[:len(lines)-1]
	}
	digits := len(fmt.Sprint(len(lines)))
	for i, line := range lines {
		lines[i] = fmt.Sprintf("%*d │ %s", digits, i+1, line)
	}
	return strings.Join(lines, "\n")
}

type scriptDetail struct {
	host    ocli.DetailHost
	detail  *ochazuke.Detail
	command Command
	script  string
	age     string
	lastRun string
	valid   bool
}

func newScriptDetail(host ocli.DetailHost) (ocli.DetailView, error) {
	parent := host.Model.Runtime.Instances[host.Parent]
	if parent == nil {
		return nil, fmt.Errorf("missing detail parent %q", host.Parent)
	}
	d := host.Model.Runtime.Detail(parent.Parent)
	if d == nil {
		return nil, fmt.Errorf("missing detail window %q", parent.Parent)
	}
	return &scriptDetail{host: host, detail: d}, nil
}

func (v *scriptDetail) Show(row ocli.Row, ok bool) error {
	v.valid = ok
	if !ok {
		v.command = Command{}
		v.script = ""
		return nil
	}
	data, e := json.Marshal(row.Data)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(data, &v.command); e != nil {
		return e
	}
	v.script = v.host.Field(row, "script")
	v.age = v.host.Field(row, "age")
	v.lastRun = v.host.Field(row, "last_run")
	return nil
}

func (v *scriptDetail) Focused() bool { return false }

func (v *scriptDetail) Block(width, height int) ochazuke.Block {
	if width <= 0 || height <= 0 {
		return ochazuke.Text("")
	}
	if !v.valid {
		return ochazuke.Text("No command selected")
	}
	c := v.command
	author := c.Author.Name
	if c.Author.Harness != "" && c.Author.Harness != author {
		author += "/" + c.Author.Harness
	}
	if c.Author.Pane != "" {
		author += "@" + c.Author.Pane
	}
	header := []string{
		fmt.Sprintf("Name: %s  Age: %s", c.Name, v.age),
		"Description: " + c.Description,
		fmt.Sprintf("Author: %s  Cwd: %s", author, c.Cwd),
		"Tags: " + strings.Join(c.Tags, ", "),
	}
	if v.lastRun != "" {
		header = append(header, "Last run: "+v.lastRun)
	}
	header = header[:min(len(header), max(0, height-2))]
	for i, line := range header {
		header[i] = ansi.Truncate(strings.ReplaceAll(line, "\n", " "), width, "…")
	}
	body, marker := v.detail.Window(scriptAtWidth(numberedScript(v.script), width), 0, max(1, height-len(header)-1))
	separator := ""
	if marker != "" {
		separator = marker
	}
	return ochazuke.Text(strings.Join(append(header, separator, body), "\n"))
}

type scriptViewer struct {
	title, text string
	detail      *ochazuke.Detail
}

func (v *scriptViewer) ModalTitle() string { return v.title }
func (v *scriptViewer) ModalBlock(styles ochazuke.Styles, width, height int) ochazuke.Block {
	body, marker := v.detail.Window(scriptAtWidth(v.text, width), 0, max(0, height-1))
	header := marker + strings.Repeat(" ", max(0, width-ansi.StringWidth(marker)))
	return ochazuke.Text(styles.Muted.Render(header) + "\n" + body)
}

// Hard wrapping retains spaces at wrap boundaries; word wrapping would alter
// shell indentation and spacing. The framework renders tabs as four spaces.
func scriptAtWidth(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Hardwrap(strings.ReplaceAll(text, "\t", "    "), width, true)
}

func registerScriptViewer(r *ochazuke.Runtime) {
	b := r.Define(scriptViewerID).Pin("normal").Mode("normal")
	for _, row := range []struct {
		key   string
		lines int
	}{{"j", 1}, {"k", -1}, {"ctrl+d", 8}, {"ctrl+u", -8}, {"g", -1_000_000_000}, {"G", 1_000_000_000}} {
		b.Bind(row.key, ochazuke.Do("ochazuke.detail.scroll", ochazuke.Arguments{"id": scriptViewerTextID, "lines": row.lines}).FromPayload("count", "count").Captured())
	}
	b.Bind("q", ochazuke.Do("ochazuke.modal.pop")).Bind("esc", ochazuke.Do("ochazuke.modal.pop"))
}

func openScriptViewer(r *ochazuke.Runtime, title, source string) error {
	inst, e := r.OpenModal(scriptViewerID, ochazuke.Payload{}, ochazuke.ReplaceOpen())
	if e != nil {
		return e
	}
	d, e := r.AddDetail(scriptViewerTextID, nil, ochazuke.Parent(inst.ID))
	if e != nil {
		_ = r.Pop()
		return e
	}
	inst.Component = &scriptViewer{title: title, text: numberedScript(source), detail: d}
	return nil
}
