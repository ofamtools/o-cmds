package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
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

func (a *Service) openInput(path, kind string) (*runInput, error) {
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
	kept := cappedStream{limit: a.maxBytes}
	n, err := io.Copy(io.MultiWriter(h, &kept), f)
	if err != nil {
		return fail(err)
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return &runInput{source: StdinSource{Kind: kind, Path: path, Bytes: n, SHA: fmt.Sprintf("%x", h.Sum(nil)), Kept: kept.Len(), Truncated: n > int64(kept.Len())}, file: f, kept: kept.Bytes()}, nil
}

func (a *Service) inputForRun(r Run) (*runInput, error) {
	switch r.Stdin.Kind {
	case "", "none":
		return nil, nil
	case "file":
		return a.openInput(r.Stdin.Path, "file")
	case "edited":
		if r.Stdin.Truncated {
			return nil, errors.New("cannot rerun: edited stdin was truncated by jobs.max-bytes")
		}
		return a.openInput(a.store.runPath(r, ".in"), "edited")
	default:
		return nil, fmt.Errorf("unknown stdin kind %q", r.Stdin.Kind)
	}
}

func (a *Service) newInputBuffer() (string, error) {
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

var errEmptyInput = errors.New("stdin cancelled: editor buffer is empty")

func editedContent(path string, editorErr error) error {
	if editorErr != nil {
		return fmt.Errorf("stdin cancelled: editor failed: %w", editorErr)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stdin cancelled: %w", err)
	}
	if info.Size() == 0 {
		return errEmptyInput
	}
	return nil
}
