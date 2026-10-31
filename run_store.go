package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Run snapshots execution identity; it deliberately does not snapshot the script.
// A nil Ended denotes an unfinished run (including an interrupted recorder).
type Run struct {
	ID           string        `json:"id"`
	CommandID    string        `json:"command_id"`
	CommandName  string        `json:"command_name"`
	ScriptHash   string        `json:"script_sha256"`
	Args         []string      `json:"args"`
	Cwd          string        `json:"cwd"`
	Started      time.Time     `json:"started"`
	Ended        *time.Time    `json:"ended,omitempty"`
	Duration     time.Duration `json:"duration_ns"`
	ExitCode     *int          `json:"exit_code"`
	Signal       string        `json:"signal,omitempty"`
	Error        string        `json:"error,omitempty"`
	Author       Author        `json:"author"`
	Source       string        `json:"source"`
	StdoutBytes  int64         `json:"stdout_bytes"`
	StderrBytes  int64         `json:"stderr_bytes"`
	StdoutKept   int           `json:"stdout_kept"`
	StderrKept   int           `json:"stderr_kept"`
	StdoutCut    bool          `json:"stdout_truncated"`
	StderrCut    bool          `json:"stderr_truncated"`
	CaptureError string        `json:"capture_error,omitempty"`
}

type RunOutput struct {
	Run
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	// JSON text replaces invalid UTF-8. These fields preserve binary streams.
	StdoutBase64 string `json:"stdout_base64,omitempty"`
	StderrBase64 string `json:"stderr_base64,omitempty"`
}

func (s *Store) runPath(r Run, ext string) string {
	return filepath.Join(s.Dir, "runs", r.CommandID, r.ID+ext)
}

func safeRunPart(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}

// Caller holds the store lock. Metadata is the commit marker for the two streams.
func (s *Store) writeRun(r Run, out, errOut []byte) error {
	if !safeRunPart(r.CommandID) || !safeRunPart(r.ID) {
		return errors.New("invalid run identity")
	}
	if err := os.MkdirAll(filepath.Dir(s.runPath(r, ".json")), 0o700); err != nil {
		return err
	}
	if err := atomicBytes(s.runPath(r, ".out"), out); err != nil {
		return err
	}
	if err := atomicBytes(s.runPath(r, ".err"), errOut); err != nil {
		return err
	}
	return atomicJSON(s.runPath(r, ".json"), r)
}

func (s *Store) runs() ([]Run, error) {
	files, err := filepath.Glob(filepath.Join(s.Dir, "runs", "*", "*.json"))
	if err != nil {
		return nil, err
	}
	rows := []Run{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var r Run
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("read run %s: %w", file, err)
		}
		if !safeRunPart(r.ID) || !safeRunPart(r.CommandID) || s.runPath(r, ".json") != file {
			return nil, fmt.Errorf("invalid run identity in %s", file)
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b Run) int {
		if n := a.Started.Compare(b.Started); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return rows, nil
}

func (s *Store) Runs(commandID string) (rows []Run, err error) {
	err = s.locked(func() error {
		rows, err = s.runs()
		if commandID != "" {
			rows = slices.DeleteFunc(rows, func(r Run) bool { return r.CommandID != commandID })
		}
		return err
	})
	return
}

func resolveRun(rows []Run, target string) (Run, error) {
	commands := make([]Command, len(rows))
	for i, r := range rows {
		commands[i] = Command{ID: r.ID, Name: r.ID}
	}
	c, err := resolve(commands, strings.TrimPrefix(target, "run/"))
	if err != nil {
		return Run{}, fmt.Errorf("run target: %w", err)
	}
	for _, r := range rows {
		if r.ID == c.ID {
			return r, nil
		}
	}
	return Run{}, errors.New("run not found")
}

func (s *Store) ReadRun(target string) (out RunOutput, err error) {
	err = s.locked(func() error {
		rows, e := s.runs()
		if e != nil {
			return e
		}
		out.Run, e = resolveRun(rows, target)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(s.runPath(out.Run, ".out"))
		if e != nil {
			return e
		}
		out.Stdout = string(b)
		if !utf8.Valid(b) {
			out.StdoutBase64 = base64.StdEncoding.EncodeToString(b)
		}
		b, e = os.ReadFile(s.runPath(out.Run, ".err"))
		out.Stderr = string(b)
		if !utf8.Valid(b) {
			out.StderrBase64 = base64.StdEncoding.EncodeToString(b)
		}
		return e
	})
	return
}

func (s *Store) removeRun(r Run) error {
	for _, ext := range []string{".json", ".out", ".err"} {
		if err := os.Remove(s.runPath(r, ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteRun(target string) error {
	return s.locked(func() error {
		rows, err := s.runs()
		if err != nil {
			return err
		}
		r, err := resolveRun(rows, target)
		if err != nil {
			return err
		}
		return s.removeRun(r)
	})
}

func (s *Store) ClearRuns(commandID string) error {
	return s.locked(func() error {
		rows, err := s.runs()
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.CommandID == commandID {
				if err := s.removeRun(r); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Store) finishRun(r Run, out, errOut []byte, keep int) error {
	return s.locked(func() error {
		// Explicit deletion/clear while running wins over completion.
		if _, err := os.Stat(s.runPath(r, ".json")); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if err := s.writeRun(r, out, errOut); err != nil {
			return err
		}
		rows, err := s.runs()
		if err != nil {
			return err
		}
		rows = slices.DeleteFunc(rows, func(v Run) bool { return v.CommandID != r.CommandID || v.Ended == nil })
		for _, old := range rows[:max(0, len(rows)-keep)] {
			if err := s.removeRun(old); err != nil {
				return err
			}
		}
		return nil
	})
}
