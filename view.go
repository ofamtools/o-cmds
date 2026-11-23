package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/markdown"
	"ofam.dev/ochazuke/widgets/picker"
)

// geom is where the list lands: its size, the data rows it shows, whether
// the detail pane sits beside it (else below) and the detail's height.
type geom struct {
	tableW, tableH, rows, detailH int
	side                          bool
}

// geometry mirrors the view's layout: a title line, the body (list, or a
// split of list and detail), the filter line while typing, and two status
// lines. The split rounds like layout.Solve.
func (env *Env) geometry(a *App, w, h int) geom {
	if w <= 0 || h <= 0 {
		w, h = 120, 32
	}
	body := h - 3
	if a.Typing {
		body--
	}
	body = max(body, 3)
	g := geom{tableW: w, tableH: body, detailH: body}
	if a.Detail {
		if w >= env.StackW {
			g.side = true
			g.tableW = int(math.Round(float64(max(0, w-1)) * a.Ratio))
		} else {
			g.tableH = int(math.Round(float64(max(0, body-1)) * a.Ratio))
			g.detailH = body - 1 - g.tableH
		}
	}
	g.rows = max(g.tableH-2, 1)
	return g
}

func textW(s string) int { return utf8.RuneCountInString(s) }

func text(t, tone string) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindText, Tone: tone, Props: map[string]any{"text": t}}
}

func clip(s string, w int) string {
	if r := []rune(s); w > 1 && len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎", "\t", " ").Replace(s)
}

// view builds the whole screen.
func (env *Env) view(s core.State) ui.ViewNode {
	a := appOf(s)
	w := s.Width
	if w <= 0 {
		w = 120
	}
	g := env.geometry(&a, s.Width, s.Height)
	l := a.cur()
	grid := l.Grid.Copy()
	grid.PageSize = g.rows
	body := grid.View(nodeList)
	if a.Detail {
		split := ui.ViewNode{ID: "body", Kind: ui.KindSplit, Ratio: a.Ratio, Gap: 1, Props: map[string]any{"axis": "horizontal"}}
		if !g.side {
			split.Props["axis"] = "vertical"
		}
		split.Children = []ui.ViewNode{body, env.detailNode(&a, g)}
		body = split
	}
	children := []ui.ViewNode{env.titleLine(&a, w), body}
	if a.Typing {
		children = append(children, a.inputNode())
	}
	children = append(children, a.infoLine(s, w), env.statusLine(&a, w))
	root := ui.ViewNode{ID: "root", Kind: ui.KindVStack, Children: []ui.ViewNode{
		{ID: "page", Kind: ui.KindVStack, Scope: a.View, Children: children},
	}}
	for _, m := range s.Modals {
		root.Children = append(root.Children, env.modalView(s, m))
	}
	return root
}

func (env *Env) titleLine(a *App, w int) ui.ViewNode {
	tabs := []struct{ name, view string }{{fmt.Sprintf(" Commands %d ", len(a.Cmds)), viewCommands}, {fmt.Sprintf(" Jobs %d ", len(a.Runs)), viewJobs}}
	line := " o-cmds "
	spans := []ui.ToneSpan{{From: 0, To: textW(line), Tone: "title"}}
	for _, t := range tabs {
		tone := "muted"
		if t.view == a.View {
			tone = "badge"
		}
		spans = append(spans, ui.ToneSpan{From: textW(line), To: textW(line) + textW(t.name), Tone: tone})
		line += t.name + " "
	}
	if n := len(a.Running); n > 0 {
		run := fmt.Sprintf(" ● %d running ", n)
		spans = append(spans, ui.ToneSpan{From: textW(line), To: textW(line) + textW(run), Tone: "badge.warning"})
		line += run
	}
	tail := " " + env.Svc.order + " · " + env.Svc.store.Dir
	spans = append(spans, ui.ToneSpan{From: textW(line), To: textW(line) + textW(tail), Tone: "muted"})
	line += tail
	n := text(clip(line, w), "title")
	n.Props["spans"] = spans
	return n
}

func (a *App) inputNode() ui.ViewNode {
	l := a.cur()
	label := "/ " + a.View + " [" + string(l.Grid.MatchMode) + "]"
	line := l.Line
	line.Sync(l.Grid.Query)
	return ui.ViewNode{ID: nodeFilter, Kind: ui.KindInput, Focusable: true, Title: label, Props: map[string]any{
		"value": l.Grid.Query, "cursor": line.Cursor,
		"placeholder": "type to filter · enter keeps · esc clears · tab match mode · ↑↓ move",
	}}
}

func (a *App) infoLine(s core.State, w int) ui.ViewNode {
	l := a.cur()
	var parts []string
	items := l.Grid.Items()
	pos := "no rows"
	if len(items) > 0 {
		pos = fmt.Sprintf("row %d/%d", l.Grid.SelectedIdx+1, len(items))
	}
	if len(l.Grid.Rows) != len(items) {
		pos += fmt.Sprintf(" (%d of %d match)", len(items), len(l.Grid.Rows))
	}
	parts = append(parts, pos)
	if a.View == viewJobs && a.JobsFor != "" {
		name := a.JobsFor
		for _, c := range a.Cmds {
			if c.ID == a.JobsFor {
				name = c.Name
			}
		}
		parts = append(parts, "jobs of "+name+" (esc: every job)")
	}
	tone := "muted"
	if l.Grid.Query != "" || a.Typing {
		parts = append(parts, fmt.Sprintf("filter %s %q", l.Grid.MatchMode, l.Grid.Query))
	}
	if l.Grid.FilterErr != "" {
		parts = append(parts, "regex error: "+l.Grid.FilterErr)
		tone = "error"
	}
	if l.Sort >= 0 {
		parts = append(parts, sortLabel(l, sortKeys[a.View]))
	}
	if in := s.Input; in.Count != nil || len(in.Prefix) > 0 {
		p := strings.Join(in.Prefix, " ")
		if in.Count != nil {
			p = strconv.Itoa(*in.Count) + " " + p
		}
		parts = append(parts, "pending "+strings.TrimSpace(p))
	}
	return text(clip(" "+strings.Join(parts, " · "), w), tone)
}

func (env *Env) statusLine(a *App, w int) ui.ViewNode {
	if a.Status != "" {
		return text(clip(" "+oneLine(a.Status), w), a.Tone)
	}
	hint := " ? help · : commands · enter run · g Y assume yes · ! terminal · g r rerun · a add · e edit · E script · d d delete · u undo · / filter · s sort · tab jobs · q quit"
	if a.View == viewJobs {
		hint = " ? help · enter output · r rerun · C cancel · d d delete · / filter · s sort · esc every job · tab commands · q quit"
	}
	return text(clip(hint, w), "muted")
}

// detailSource is the detail pane's markdown for the focused record.
func (env *Env) detailSource(a *App) string {
	if a.View == viewJobs {
		j, ok := a.focusedJob()
		if !ok {
			return "_no jobs_"
		}
		if a.JobOut != nil && a.JobOut.ID == j.ID {
			return jobMarkdown(*a.JobOut, j.Number)
		}
		return jobMarkdown(RunOutput{Run: j.Run}, j.Number)
	}
	row, ok := a.focusedCmd()
	if !ok {
		return "_no commands_ — **a** adds one, or `o-cmds add` from a shell"
	}
	return cmdMarkdown(row, a.Now)
}

func (env *Env) detailNode(a *App, g geom) ui.ViewNode {
	title := "Script"
	if a.View == viewJobs {
		title = "Output"
	}
	m := markdown.New(title, env.detailSource(a))
	m.Offset, m.PageSize = a.DetailOff, max(g.detailH-2, 1)
	return m.View("detail")
}

// fenced puts body in a code block longer than any backtick run inside.
func fenced(body, lang string) string {
	longest, run := 0, 0
	for _, r := range body {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + lang + "\n" + strings.TrimSuffix(body, "\n") + "\n" + fence + "\n"
}

func cmdMarkdown(r cmdRow, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", r.Name)
	if r.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", r.Description)
	}
	num := "-"
	if r.Number > 0 {
		num = strconv.Itoa(r.Number)
	}
	fmt.Fprintf(&b, "- **Number:** %s · **Status:** %s · **Runs:** %d", num, r.Status.Status, r.Status.Count)
	if r.Status.Last != nil {
		fmt.Fprintf(&b, " (last %s ago)", age(*r.Status.Last, now))
	}
	b.WriteString("\n")
	if r.Pinned {
		b.WriteString("- **Pinned**\n")
	}
	fmt.Fprintf(&b, "- **Author:** %s\n", r.Author)
	cwd := r.Cwd
	if cwd == "" {
		cwd = "(where o-cmds runs)"
	}
	fmt.Fprintf(&b, "- **Cwd:** %s\n", cwd)
	if len(r.Tags) > 0 {
		fmt.Fprintf(&b, "- **Tags:** %s\n", strings.Join(r.Tags, ", "))
	}
	fmt.Fprintf(&b, "- **Created:** %s (%s)\n", r.Created.Local().Format("2006-01-02 15:04"), age(r.Created, now))
	if r.LastRun != nil {
		fmt.Fprintf(&b, "- **Last job:** %s\n", runSummary(*r.LastRun))
	}
	fmt.Fprintf(&b, "- **Path:** %s\n\n## Script\n\n", r.Path)
	b.WriteString(scriptMarkdown(r.Script, r.Path))
	return b.String()
}

func jobMarkdown(r RunOutput, number int) string {
	var b strings.Builder
	name := r.CommandName
	if r.Script != nil {
		name = "line"
	}
	fmt.Fprintf(&b, "# Job #%d · %s\n\n", number, name)
	dur := "-"
	if r.Ended != nil {
		dur = r.Duration.Round(time.Millisecond).String()
	}
	fmt.Fprintf(&b, "- **State:** %s · **Exit:** %s · **Duration:** %s\n", runState(r.Run), runExit(r.Run), dur)
	fmt.Fprintf(&b, "- **Started:** %s · **Source:** %s\n", r.Started.Local().Format("2006-01-02 15:04:05"), r.Source)
	if len(r.Args) > 0 {
		fmt.Fprintf(&b, "- **Args:** %s\n", strings.Join(r.Args, " "))
	}
	fmt.Fprintf(&b, "- **Cwd:** %s\n", r.Cwd)
	if r.AssumeYes {
		b.WriteString("- **Assume yes:** OCHAZUKE_ASSUME_YES=1\n")
	}
	if r.Stdin.Kind != "" && r.Stdin.Kind != "none" {
		fmt.Fprintf(&b, "- **Stdin:** %s %s (%d B)\n", r.Stdin.Kind, r.Stdin.Path, r.Stdin.Bytes)
	}
	cut := ""
	if r.StdoutCut || r.StderrCut {
		cut = " · truncated by jobs.max-bytes"
	}
	fmt.Fprintf(&b, "- **Bytes:** stdout %d · stderr %d%s\n", r.StdoutBytes, r.StderrBytes, cut)
	if r.Error != "" {
		fmt.Fprintf(&b, "- **Error:** %s\n", r.Error)
	}
	if r.CaptureError != "" {
		fmt.Fprintf(&b, "- **Capture:** %s\n", r.CaptureError)
	}
	if r.Script != nil {
		b.WriteString("\n## Line\n\n" + fenced(*r.Script, "sh"))
	}
	b.WriteString("\n## stdout\n\n")
	if r.Ended == nil && r.Stdout == "" {
		b.WriteString("_running…_\n")
	} else {
		b.WriteString(fenced(r.Stdout, "text"))
	}
	if r.Stderr != "" {
		b.WriteString("\n## stderr\n\n" + fenced(r.Stderr, "text"))
	}
	if r.Input != "" {
		b.WriteString("\n## stdin (retained)\n\n" + fenced(r.Input, "text"))
	}
	return b.String()
}

// formWindow is a view-only copy of m showing as many fields as fit.
func formWindow(m *form.Model, h, errLines int) *form.Model {
	if h <= 0 {
		h = 32
	}
	n := len(m.Specs)
	size := max(h-2-1-2-errLines, 1)
	if n <= size {
		return m
	}
	start := min(max(m.ActiveIdx-size/2, 0), n-size)
	w := *m
	w.Specs = m.Specs[start : start+size]
	w.ActiveIdx = m.ActiveIdx - start
	w.Title = fmt.Sprintf("%s · fields %d-%d of %d", m.Title, start+1, start+size, n)
	return &w
}

func (env *Env) modalView(s core.State, f core.Frame) ui.ViewNode {
	switch f.ID {
	case modalPalette:
		m, _ := f.Data.(*picker.Model)
		if m == nil {
			break
		}
		return ui.ViewNode{ID: "palette.overlay", Kind: ui.KindOverlay, Scope: "palette", Title: "Command palette · " + string(m.MatchMode),
			Props:    map[string]any{"widthPct": 70, "heightPct": 70},
			Children: []ui.ViewNode{m.View(modalPalette), text("enter runs · esc closes · tab match mode · ↑↓ move · ctrl+a/e/k/u/w edit", "muted")}}
	case modalHelp:
		off, _ := f.Data.(int)
		lines := env.helpLines()
		off = min(max(off, 0), len(lines))
		return ui.ViewNode{ID: modalHelp, Kind: ui.KindOverlay, Scope: "help", Focusable: true, Title: "Help · active key bindings",
			Props:    map[string]any{"widthPct": 80, "heightPct": 85},
			Children: []ui.ViewNode{text(strings.Join(lines[off:], "\n"), "text")}}
	case modalViewer:
		v, ok := f.Data.(viewerData)
		if !ok {
			break
		}
		m := markdown.New(v.Title, v.Source)
		m.Offset, m.PageSize = v.Offset, max(s.Height*9/10-4, 1)
		node := m.View(modalViewer)
		node.Scope = "viewer"
		return ui.ViewNode{ID: "viewer.overlay", Kind: ui.KindOverlay, Scope: "viewer", Title: v.Title,
			Props:    map[string]any{"widthPct": 90, "heightPct": 90},
			Children: []ui.ViewNode{node, text("j/k scroll · space/ctrl+f page · g g/G top/bottom · esc/q closes", "muted")}}
	case modalForm:
		cf, ok := f.Data.(cmdForm)
		if !ok {
			break
		}
		var errs []ui.ViewNode
		for _, sp := range cf.M.Specs {
			if e := cf.M.Errors[sp.Name]; e != "" {
				errs = append(errs, text(sp.Name+": "+e, "error"))
			}
		}
		win := formWindow(cf.M, s.Height, len(errs))
		kids := append([]ui.ViewNode{win.View(modalForm)}, errs...)
		kids = append(kids, text("type to edit · ctrl+a/e/k/u/w readline · tab/↑↓ field · enter saves · esc cancels", "muted"))
		return ui.ViewNode{ID: "form.overlay", Kind: ui.KindOverlay, Scope: "form", Title: win.Title, Props: map[string]any{"widthPct": 70}, Children: kids}
	}
	return ui.ViewNode{ID: f.ID + ".overlay", Kind: ui.KindOverlay, Children: []ui.ViewNode{text(f.ID, "muted")}}
}

// --- live help ------------------------------------------------------------------

type keyHelp struct{ Keys, Command, Desc string }

var helpSections = []struct {
	mode, title string
	chain       []core.ContextFrame
}{
	{"normal", "Commands table", []core.ContextFrame{{Scope: viewCommands}, {Scope: "app"}}},
	{"normal", "Jobs table (tab)", []core.ContextFrame{{Scope: viewJobs}, {Scope: "app"}}},
	{"input", "Filter input (/)", []core.ContextFrame{{Scope: viewCommands}, {Scope: "app"}}},
	{"palette", "Command palette (:)", []core.ContextFrame{{Scope: "palette"}, {Scope: "app"}}},
	{"form", "Add / edit form (a / e)", []core.ContextFrame{{Scope: "form"}, {Scope: "app"}}},
	{"viewer", "Viewer (v / enter on a job)", []core.ContextFrame{{Scope: "viewer"}, {Scope: "app"}}},
	{"help", "Help", []core.ContextFrame{{Scope: "help"}, {Scope: "app"}}},
}

var classSamples = map[string]string{core.ClassDigit0: "0", core.ClassDigit: "5", core.ClassAlpha: "x", core.ClassPrintable: "x", core.ClassPaste: core.PasteKey, core.ClassAny: "x"}

func resolveWinner(km *core.Keymap, chain []core.ContextFrame, mode string, keys []string) *core.Binding {
	in := core.InputState{}
	for i, k := range keys {
		res := km.Resolve(core.KeyQuery{Chain: chain, Mode: mode, Input: in, Key: k, Text: k})
		in = res.Input
		last := i == len(keys)-1
		switch {
		case !last && res.Outcome == core.Pending:
		case last && (res.Outcome == core.Matched || res.Outcome == core.PushedLayer):
			return res.Trace.Winner
		case last && res.Outcome == core.Pending && res.Trace.Winner != nil:
			return res.Trace.Winner
		default:
			return nil
		}
	}
	return nil
}

// activeBindings lists the bindings of mode that win resolution in chain,
// so user overrides and unbinds show exactly as they behave.
func (env *Env) activeBindings(mode string, chain []core.ContextFrame) []keyHelp {
	var out []keyHelp
	seen := map[string]bool{}
	for _, bd := range env.Bindings {
		if bd.Hidden || bd.Unbind || (bd.Mode != mode && bd.Mode != "") || bd.Command == "" && bd.Layer == nil {
			continue
		}
		keys, label := bd.Keys, strings.Join(bd.Keys, " ")
		if len(keys) == 0 {
			sample, ok := classSamples[bd.Class]
			if !ok {
				continue
			}
			keys, label = []string{sample}, "‹"+bd.Class+"›"
			if bd.Class == core.ClassDigit0 {
				label = "0"
			}
		}
		if seen[label] {
			continue
		}
		win := resolveWinner(env.Km, chain, mode, keys)
		if win == nil || win.Command != bd.Command || !equalKeys(win.Keys, bd.Keys) || win.Class != bd.Class || win.Scope != bd.Scope {
			continue
		}
		seen[label] = true
		desc := bd.Hint
		if spec, ok := env.Km.Commands.Lookup(bd.Command); ok && spec.Description != "" {
			desc = spec.Description
		}
		out = append(out, keyHelp{Keys: label, Command: bd.Command, Desc: desc})
	}
	return out
}

func equalKeys(a, b []string) bool { return strings.Join(a, " ") == strings.Join(b, " ") }

func keysByCommand(env *Env, mode string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	for _, sec := range helpSections {
		if sec.mode != mode {
			continue
		}
		for _, k := range env.activeBindings(mode, sec.chain) {
			if !seen[k.Command+"|"+k.Keys] {
				seen[k.Command+"|"+k.Keys] = true
				out[k.Command] = append(out[k.Command], k.Keys)
			}
		}
	}
	return out
}

func (env *Env) helpLines() []string {
	var lines []string
	for _, sec := range helpSections {
		bs := env.activeBindings(sec.mode, sec.chain)
		if len(bs) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sec.title)
		for _, k := range bs {
			lines = append(lines, fmt.Sprintf("  %-12s %s  (%s)", k.Keys, k.Desc, k.Command))
		}
	}
	lines = append(lines, "",
		"enter runs captured in the background (stdin is empty); ! runs on the terminal with the UI suspended.",
		"Every run is recorded as a job (settings.json jobs.keep); g Y sets OCHAZUKE_ASSUME_YES=1 and reruns repeat it.",
		"Bindings, theme and settings load from $XDG_CONFIG_HOME/o-cmds or ~/.config/o-cmds.")
	for _, n := range env.Notes {
		lines = append(lines, "config: "+n)
	}
	return lines
}
