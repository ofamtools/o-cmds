package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

type page struct {
	a     *App
	m     *ochazuke.Model
	v     *ocli.PageView
	dirty bool
}

type reloadMsg struct{}

func (p *page) tick() tea.Cmd {
	every := p.a.r.Settings().Duration("list.refresh")
	if every <= 0 {
		return nil
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return reloadMsg{} })
}

func mountPage(a *App) (*page, error) {
	return mountPageAs(a, func(ocli.CatalogDoc) ocli.CatalogPage { return standalonePage(a.r) }, ocli.PageConfig{DetailKinds: map[string]ocli.DetailFactory{scriptDetailKind: newScriptDetail}})
}

// mountPageAs mounts the commands page from the published catalog with the
// page spec and detail kinds a mount supplies (standalone, or a host's).
func mountPageAs(a *App, spec func(ocli.CatalogDoc) ocli.CatalogPage, cfg ocli.PageConfig) (*page, error) {
	data, e := ocli.Catalog(a.r, "o-cmds", version)
	if e != nil {
		return nil, e
	}
	var doc ocli.CatalogDoc
	if e = json.Unmarshal(data, &doc); e != nil {
		return nil, e
	}
	m := ochazuke.NewModel(a.r)
	m.Title = "o-cmds"
	v, e := ocli.MountPage(m, doc, spec(doc), cfg)
	if e != nil {
		return nil, e
	}
	p := &page{a: a, m: m, v: v}
	a.tui = true
	a.changed = func() { p.dirty = true }
	a.r.Settings().Observe("list.order", func(string, any) { p.dirty = true })
	quit := ochazuke.Do("app.quit")
	quit.Instance = pageID
	quit.Mode = "normal"
	quit.Keys = "q"
	if e = a.r.Bind(quit); e != nil {
		return nil, e
	}
	if e = a.r.OpenPage(pageID); e != nil {
		return nil, e
	}
	if e = p.reload(true); e != nil {
		return nil, e
	}
	if e = v.List(listID).Focus(); e != nil {
		return nil, e
	}
	return p, nil
}

func (p *page) reload(initial bool) error {
	rows, e := p.a.rows("")
	if e != nil {
		return e
	}
	if e = p.v.SetRows(listID, rows); e != nil {
		return e
	}
	if initial && len(rows) > 0 && p.a.r.Settings().Bool("list.start-newest") {
		i := len(rows) - 1
		if p.a.newestFirst() {
			i = 0
		}
		return p.v.List(listID).Select(listID + "/" + rows[i].ID)
	}
	return nil
}

func runTUI(a *App, args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	smoke := fs.Bool("smoke", false, "Render one frame and exit")
	w := fs.Int("width", 120, "Frame width")
	h := fs.Int("height", 30, "Frame height")
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if fs.NArg() > 0 {
		return errors.New("unexpected view arguments")
	}
	p, e := mountPage(a)
	if e != nil {
		return e
	}
	if *smoke {
		p.m.Width, p.m.Height = *w, *h
		_, e = fmt.Fprintln(a.out, strings.TrimRight(ansi.Strip(p.m.View().Content), "\n"))
		return e
	}
	_, e = tea.NewProgram(p, tea.WithInput(a.in), tea.WithOutput(a.out)).Run()
	return e
}

func (p *page) Init() tea.Cmd { return tea.Batch(p.m.Init(), p.a.r.PendingCommands(), p.tick()) }
func (p *page) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(reloadMsg); ok {
		if e := p.reload(false); e != nil {
			p.a.r.UI().Status(e.Error())
		}
		return p, p.tick()
	}
	_, cmd := p.m.Update(msg)
	if p.dirty {
		p.dirty = false
		if e := p.reload(false); e != nil {
			p.a.r.UI().Status(e.Error())
		}
		cmd = tea.Batch(cmd, p.a.r.PendingCommands())
	}
	return p, cmd
}
func (p *page) View() tea.View { return p.m.View() }
