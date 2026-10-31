package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"ofam.dev/ochazuke"
)

// StdinSource describes an explicit input. "none" leaves stdin inherited.
// Bytes and SHA describe the full source at preparation, not just its kept prefix.
type StdinSource struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Bytes     int64  `json:"bytes"`
	SHA       string `json:"sha,omitempty"`
	Kept      int    `json:"kept"`
	Truncated bool   `json:"truncated"`
}

type runInput struct {
	source StdinSource
	file   *os.File
	kept   []byte
}

func (in *runInput) close() {
	if in != nil {
		_ = in.file.Close()
	}
}

func (a *App) openInput(path, kind string) (*runInput, error) {
	if path == "" {
		return nil, nil
	}
	if kind != "file" && kind != "edited" {
		return nil, errors.New("stdin kind must be file or edited")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	fail := func(err error) (*runInput, error) { _ = f.Close(); return nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(errors.New("stdin must be a regular file"))
	}
	h := sha256.New()
	kept := cappedStream{limit: a.r.Settings().Int("runs.max-bytes")}
	n, err := io.Copy(io.MultiWriter(h, &kept), f)
	if err != nil {
		return fail(err)
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return &runInput{source: StdinSource{Kind: kind, Path: path, Bytes: n, SHA: fmt.Sprintf("%x", h.Sum(nil)), Kept: kept.Len(), Truncated: n > int64(kept.Len())}, file: f, kept: kept.Bytes()}, nil
}

func (a *App) inputForRun(r Run) (*runInput, error) {
	switch r.Stdin.Kind {
	case "", "none":
		return nil, nil
	case "file":
		return a.openInput(r.Stdin.Path, "file")
	case "edited":
		if r.Stdin.Truncated {
			return nil, errors.New("cannot rerun: edited stdin was truncated by runs.max-bytes")
		}
		return a.openInput(a.store.runPath(r, ".in"), "edited")
	default:
		return nil, fmt.Errorf("unknown stdin kind %q", r.Stdin.Kind)
	}
}

func (a *App) clearInputNotice() {
	if a.stdinNotice != 0 {
		a.r.UI().ClearNotice(a.stdinNotice)
		a.stdinNotice = 0
	}
}

func (a *App) stdinFile(_ ochazuke.Ctx, row Command) ochazuke.Result {
	a.clearInputNotice()
	if !a.tui {
		return ochazuke.Failed(errors.New("use cmd run --stdin FILE"))
	}
	if !a.r.Settings().Bool("view.stdin") {
		return ochazuke.Failed(errors.New("stdin picker disabled by view.stdin"))
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ochazuke.Failed(err)
	}
	_, err = a.r.OpenPathChooser(ochazuke.PathChooserSpec{
		Kind: "stdin-file", Title: "Stdin file: " + row.Name,
		Pick: ochazuke.PathPick{Name: "stdin", Parameter: ochazuke.Parameter{Type: "path", Path: ochazuke.PathFile, Base: cwd}},
		Choose: func(path string) ochazuke.Result {
			in, err := a.openInput(path, "file")
			if err != nil {
				return ochazuke.Failed(err)
			}
			return a.executeInput(row, nil, in)
		},
	})
	return result(nil, err)
}

func (a *App) recentInputs(ochazuke.PathPick) []string {
	rows, err := a.store.Runs("")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	paths := []string{}
	for i := len(rows) - 1; i >= 0; i-- {
		source := rows[i].Stdin
		if source.Kind == "file" && !seen[source.Path] {
			paths = append(paths, source.Path)
			seen[source.Path] = true
		}
	}
	return paths
}

func (a *App) newInputBuffer() (string, error) {
	dir := filepath.Join(a.store.Dir, "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "stdin-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if err = f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func editedContent(path string, editorErr error) error {
	if editorErr != nil {
		return fmt.Errorf("stdin cancelled: editor failed: %w", editorErr)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stdin cancelled: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("stdin cancelled: editor buffer is empty")
	}
	return nil
}

func (a *App) editStdin(c ochazuke.Ctx, row Command) ochazuke.Result {
	if !a.tui {
		return result(nil, a.runEdited(background(), row, stringList(c.Args["args"])))
	}
	if !a.r.Settings().Bool("view.stdin") {
		return ochazuke.Failed(errors.New("stdin editor disabled by view.stdin"))
	}
	a.clearInputNotice()
	path, err := a.newInputBuffer()
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Foreground(editorCommand(background(), path)).Then(func(c ochazuke.Ctx, _ any, editorErr error) {
		defer os.Remove(path)
		if err := editedContent(path, editorErr); err != nil {
			if editorErr != nil {
				a.stdinNotice = c.Notify(err.Error())
			} else {
				c.Info(err.Error())
			}
			return
		}
		args := []any{}
		for _, arg := range stringList(c.Args["args"]) {
			args = append(args, arg)
		}
		// Execute synchronously prepares/opens the input before the temp name is removed.
		_, err := a.r.ExecuteAt(c.Context, ochazuke.Do("cmd.run", ochazuke.Arguments{"target": row.ID, "args": args, "stdin": path, "stdin-kind": "edited"}).Request)
		if err != nil {
			c.Error(err.Error())
		}
	})
}
