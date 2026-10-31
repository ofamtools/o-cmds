package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// cappedStream counts all observed bytes while retaining only the prefix.
// It never stops draining when the cap is reached.
type cappedStream struct {
	bytes.Buffer
	limit int
	total int64
}

func (w *cappedStream) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	_, _ = w.Buffer.Write(p[:min(len(p), max(0, w.limit-w.Len()))])
	return len(p), nil
}

type captureStream struct {
	data          cappedStream
	live          io.Writer
	master, slave *os.File
	done          chan struct{}
	copyErr       error
	resize        chan os.Signal
	stop          chan struct{}
	stopped       chan struct{}
}

type lockedWriter struct {
	mu  *sync.Mutex
	dst io.Writer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dst.Write(p)
}

func newCaptureStream(live io.Writer, limit int, mu *sync.Mutex) (*captureStream, error) {
	s := &captureStream{data: cappedStream{limit: limit}, live: lockedWriter{mu, live}}
	f, ok := live.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return s, nil
	}
	m, slave, err := pty.Open()
	if err != nil {
		return nil, err
	}
	// Rewrap a nonblocking descriptor so os.File uses the runtime poller.
	// Close can then interrupt a read when a descendant retains the slave.
	fd, err := unix.FcntlInt(m.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		err = unix.SetNonblock(fd, true)
	}
	if err != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		_ = m.Close()
		_ = slave.Close()
		return nil, err
	}
	_ = m.Close()
	m = os.NewFile(uintptr(fd), "run-output-pty")
	s.master, s.slave = m, slave
	// Raw output avoids the PTY adding CRs to bytes already destined for a tty.
	// Stdin remains the original terminal, with its original line discipline.
	if _, err := term.MakeRaw(slave.Fd()); err != nil {
		_ = m.Close()
		_ = slave.Close()
		return nil, err
	}
	resize := func() {
		size, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
		if err != nil {
			return
		}
		conn, err := m.SyscallConn()
		if err != nil {
			return
		}
		_ = conn.Control(func(fd uintptr) { _ = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, size) })
	}
	resize()
	s.resize, s.stop, s.stopped = make(chan os.Signal, 1), make(chan struct{}), make(chan struct{})
	signal.Notify(s.resize, syscall.SIGWINCH)
	go func() {
		defer close(s.stopped)
		for {
			select {
			case <-s.resize:
				resize()
			case <-s.stop:
				return
			}
		}
	}()
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_, s.copyErr = io.Copy(io.MultiWriter(&s.data, s.live), m)
		// Linux PTYs report EIO for the ordinary end of the slave.
		if errors.Is(s.copyErr, syscall.EIO) {
			s.copyErr = nil
		}
	}()
	return s, nil
}

func (s *captureStream) writer() io.Writer {
	if s.slave != nil {
		return s.slave
	}
	return io.MultiWriter(&s.data, s.live)
}

func (s *captureStream) close() error {
	if s.master == nil {
		return nil
	}
	signal.Stop(s.resize)
	close(s.stop)
	<-s.stopped
	_ = s.slave.Close()
	// A background descendant can retain the slave beyond the shell's exit.
	// Bound draining so such a process cannot hold the CLI/TUI open forever.
	var err error
	select {
	case <-s.done:
	case <-time.After(time.Second):
		err = errors.New("output drain timed out; a descendant may still hold the stream")
		_ = s.master.Close()
		<-s.done
	}
	_ = s.master.Close()
	return errors.Join(err, s.copyErr)
}

// prepareRun is shared by CLI and foreground/catalog execution. The returned
// completion must run after Cmd.Run/Wait, including start failures.
func (a *App) prepareRun(cmd *exec.Cmd, c Command, args []string, source string, inputs ...*runInput) (func(error) error, error) {
	// The durable count goes up whatever runs.keep retains. Counting never
	// blocks a run: its error is reported with the run's completion.
	countErr := a.store.CountRun(c.ID, time.Now())
	if countErr != nil {
		countErr = fmt.Errorf("count run: %w", countErr)
	}
	a.refresh()
	keep := a.r.Settings().Int("runs.keep")
	if keep == 0 {
		return func(error) error { return countErr }, nil
	}
	script, err := a.store.Script(c)
	if err != nil {
		return nil, err
	}
	id, err := token()
	if err != nil {
		return nil, err
	}
	at := time.Now()
	cwd := cmd.Dir
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	r := Run{ID: at.UTC().Format("20060102T150405.000000000Z") + "-" + id, CommandID: c.ID, CommandName: c.Name,
		ScriptHash: fmt.Sprintf("%x", sha256.Sum256([]byte(script))), Args: append([]string{}, args...), Cwd: cwd,
		Started: at.UTC(), Source: source, Author: stampAuthor(os.Getenv, "owner")}
	r.Stdin.Kind = "none"
	var input []byte
	if len(inputs) > 0 && inputs[0] != nil {
		r.Stdin = inputs[0].source
		input = inputs[0].kept
		if r.Stdin.Kind == "edited" {
			r.Stdin.Path = a.store.runPath(r, ".in")
		}
	}
	if err := a.store.locked(func() error {
		if r.Stdin.Kind == "none" {
			return a.store.writeRun(r, nil, nil)
		}
		return a.store.writeRun(r, nil, nil, input)
	}); err != nil {
		return nil, err
	}
	limit := a.r.Settings().Int("runs.max-bytes")
	mu := &sync.Mutex{}
	out, err := newCaptureStream(a.out, limit, mu)
	if err != nil {
		return nil, a.failedCapture(r, err, keep)
	}
	errOut, err := newCaptureStream(a.errOut, limit, mu)
	if err != nil {
		_ = out.close()
		return nil, a.failedCapture(r, err, keep)
	}
	cmd.Stdout, cmd.Stderr = out.writer(), errOut.writer()
	// Bound exec's own pipe draining for redirected output and inherited stdin.
	cmd.WaitDelay = time.Second
	var once sync.Once
	var finishErr error
	return func(runErr error) error {
		once.Do(func() {
			captureErr := errors.Join(out.close(), errOut.close())
			ended := time.Now().UTC()
			r.Ended, r.Duration = &ended, time.Since(at)
			if cmd.ProcessState != nil {
				if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					r.Signal = status.Signal().String()
				} else {
					code := cmd.ProcessState.ExitCode()
					r.ExitCode = &code
				}
			} else if runErr == nil {
				code := 0
				r.ExitCode = &code
			}
			if runErr != nil {
				r.Error = runErr.Error()
			}
			if captureErr != nil {
				r.CaptureError = captureErr.Error()
			}
			r.StdoutBytes, r.StderrBytes = out.data.total, errOut.data.total
			r.StdoutKept, r.StderrKept = out.data.Len(), errOut.data.Len()
			r.StdoutCut, r.StderrCut = r.StdoutBytes > int64(r.StdoutKept), r.StderrBytes > int64(r.StderrKept)
			finishErr = errors.Join(countErr, a.store.finishRun(r, out.data.Bytes(), errOut.data.Bytes(), keep))
		})
		return finishErr
	}, nil
}

func (a *App) failedCapture(r Run, err error, keep int) error {
	ended := time.Now().UTC()
	r.Ended, r.Error = &ended, err.Error()
	r.Duration = ended.Sub(r.Started)
	return errors.Join(err, a.store.finishRun(r, nil, nil, keep))
}
