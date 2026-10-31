package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
)

// Command metadata lives beside an ordinary, owner-editable shell script.
// IDs and paths stay stable when a command is renamed.
type Command struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Created     time.Time `json:"created"`
	Author      Author    `json:"author"`
	Cwd         string    `json:"cwd,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Pinned      bool      `json:"pinned"`
	Deleted     bool      `json:"deleted,omitempty"`
	Path        string    `json:"path"`
}

type Author struct {
	Name    string `json:"name"`
	Harness string `json:"harness,omitempty"`
	Pane    string `json:"pane,omitempty"`
	Session string `json:"session,omitempty"`
}

func stampAuthor(getenv func(string) string, fallback string) Author {
	first := func(keys ...string) string {
		for _, key := range keys {
			if v := getenv(key); v != "" {
				return v
			}
		}
		return ""
	}
	a := Author{Name: first("O_CMDS_AUTHOR", "TUIOS_AGENT_NAME", "AGENT_NAME"), Harness: first("TUIOS_HARNESS", "AGENT_HARNESS"), Pane: first("TUIOS_PANE_ID"), Session: first("TUIOS_SESSION")}
	if a.Harness == "" {
		if getenv("CODEX_THREAD_ID") != "" || getenv("CODEX_CI") != "" {
			a.Harness = "codex"
		}
		if getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
			a.Harness = "claude-code"
		}
	}
	if a.Name == "" {
		a.Name = a.Harness
	}
	if a.Name == "" {
		a.Name = fallback
	}
	if a.Name == "" {
		a.Name = "owner"
	}
	return a
}

type Store struct {
	Dir string
	Now func() time.Time
}

func dataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Abs(filepath.Join(base, "o-cmds"))
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// A process lock serializes name allocation, metadata updates and undo across
// independent CLI/MCP processes. Readers also use it for consistent snapshots.
func (s *Store) locked(fn func() error) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return fn()
}

func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
		if b.Len() >= 72 {
			break
		}
	}
	if b.Len() == 0 {
		return "command"
	}
	return b.String()
}

func token() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Failed writes leave a hidden recovery file rather than deleting user bytes.
func atomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicBytes(path, append(b, '\n'))
}

func atomicBytes(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (s *Store) path(id string) string { return filepath.Join(s.Dir, id+".sh") }
func (s *Store) metadata(c Command) error {
	c.Path = s.path(c.ID)
	return atomicJSON(filepath.Join(s.Dir, c.ID+".json"), c)
}

func (s *Store) list() ([]Command, error) {
	files, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	out := []Command{}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".sh") || strings.HasPrefix(f.Name(), ".") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		id := strings.TrimSuffix(f.Name(), ".sh")
		c := Command{ID: id, Name: id, Created: info.ModTime().UTC(), Author: Author{Name: "unassociated"}}
		if b, err := os.ReadFile(filepath.Join(s.Dir, id+".json")); err == nil {
			var saved Command
			if json.Unmarshal(b, &saved) == nil {
				c = saved
			}
		}
		c.ID = id
		c.Path = s.path(id)
		if !c.Deleted {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Command) int {
		if n := a.Created.Compare(b.Created); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (s *Store) List() (out []Command, err error) {
	err = s.locked(func() error { out, err = s.list(); return err })
	return
}

func uniqueName(name string, rows []Command, except string) string {
	if strings.TrimSpace(name) == "" {
		name = "command"
	}
	used := map[string]bool{}
	for _, c := range rows {
		if c.ID != except {
			used[c.Name] = true
		}
	}
	base := name
	for n := 2; used[name]; n++ {
		name = base + "-" + strconv.Itoa(n)
	}
	return name
}

type Add struct {
	Name, Description, Script, Cwd string
	Tags                           []string
	Author                         Author
}

func (s *Store) Add(a Add) (c Command, err error) {
	err = s.locked(func() error {
		rows, e := s.list()
		if e != nil {
			return e
		}
		name := uniqueName(a.Name, rows, "")
		suffix, e := token()
		if e != nil {
			return e
		}
		at := s.now()
		if len(rows) > 0 && !at.After(rows[len(rows)-1].Created) {
			at = rows[len(rows)-1].Created.Add(time.Nanosecond)
		}
		id := at.Format("20060102T150405.000000000Z") + "-" + slug(name) + "-" + suffix
		if a.Author.Name == "" {
			a.Author.Name = "owner"
		}
		c = Command{ID: id, Name: name, Description: a.Description, Created: at, Author: a.Author, Cwd: a.Cwd, Tags: a.Tags, Path: s.path(id)}
		if c.Cwd != "" {
			c.Cwd, e = filepath.Abs(c.Cwd)
			if e != nil {
				return e
			}
		}
		f, e := os.OpenFile(c.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if e != nil {
			return e
		}
		_, e = f.WriteString(a.Script)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		// The script is already durable if metadata fails; list recovers it
		// as an unassociated command, so recording never loses the script.
		return s.metadata(c)
	})
	return
}

func resolve(rows []Command, target string) (Command, error) {
	// Names win over numbers, so a command called "2" remains reachable.
	for _, c := range rows {
		if c.Name == target || c.ID == strings.TrimPrefix(target, "cmd/") {
			return c, nil
		}
	}
	if n, e := strconv.Atoi(target); e == nil && n > 0 && n <= len(rows) {
		return rows[n-1], nil
	}
	return Command{}, fmt.Errorf("command %q not found", target)
}

func (s *Store) Resolve(target string, newestFirst bool) (Command, error) {
	rows, err := s.List()
	if err != nil {
		return Command{}, err
	}
	if newestFirst {
		slices.Reverse(rows)
	}
	return resolve(rows, target)
}

func (s *Store) Script(c Command) (string, error) {
	b, e := os.ReadFile(s.path(c.ID))
	return string(b), e
}

type Snapshot struct {
	Command Command   `json:"command"`
	Script  string    `json:"script"`
	At      time.Time `json:"at"`
	Undone  bool      `json:"undone"`
}

func (s *Store) checkpoint(c Command) error {
	text, err := s.Script(c)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.Dir, ".history")
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var next uint64 = 1
	for _, file := range files {
		n, err := strconv.ParseUint(strings.TrimSuffix(file.Name(), ".json"), 10, 64)
		if err == nil && n >= next {
			next = n + 1
		}
	}
	return atomicJSON(filepath.Join(dir, fmt.Sprintf("%020d.json", next)), Snapshot{Command: c, Script: text, At: s.now()})
}

func (s *Store) Change(id string, change func(*Command, []Command)) (c Command, err error) {
	err = s.locked(func() error {
		rows, e := s.list()
		if e != nil {
			return e
		}
		c, e = resolve(rows, id)
		if e != nil {
			return e
		}
		if e = s.checkpoint(c); e != nil {
			return e
		}
		change(&c, rows)
		return s.metadata(c)
	})
	return
}

func (s *Store) Undo() (c Command, err error) {
	err = s.locked(func() error {
		dir := filepath.Join(s.Dir, ".history")
		files, e := os.ReadDir(dir)
		if errors.Is(e, os.ErrNotExist) {
			return errors.New("nothing to undo")
		}
		if e != nil {
			return e
		}
		// Snapshot timestamps determine history order; random IDs only break ties.
		for i := len(files) - 1; i >= 0; i-- {
			f := files[i]
			if !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, f.Name())
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var snap Snapshot
			if e = json.Unmarshal(b, &snap); e != nil {
				return e
			}
			if snap.Undone {
				continue
			}
			c = snap.Command
			rows, e := s.list()
			if e != nil {
				return e
			}
			c.Name = uniqueName(c.Name, rows, c.ID)
			if e = atomicBytes(s.path(c.ID), []byte(snap.Script)); e != nil {
				return e
			}
			if e = s.metadata(c); e != nil {
				return e
			}
			snap.Undone = true
			return atomicJSON(path, snap)
		}
		return errors.New("nothing to undo")
	})
	return
}
