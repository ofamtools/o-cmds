package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func fileParam(description string) ochazuke.Parameter {
	return ochazuke.Parameter{Type: "path", Path: ochazuke.PathFile, Description: description}
}

func dirParam(description string) ochazuke.Parameter {
	return ochazuke.Parameter{Type: "path", Path: ochazuke.PathDir, Description: description}
}

func commandTarget() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Noun: "cmd", From: "row:id", Description: "Command name, number or stable ID"}
}

func (a *App) fileSpecs() []verbSpec {
	return []verbSpec{
		{name: "import", creates: true, description: "Import script text from a file, stdin or captured command output; never execute the imported script", params: map[string]ochazuke.Parameter{
			"file": fileParam("Source script file, or - for stdin"), "command": param("Shell command whose stdout becomes the script"),
			"name": param("Name; defaults to the source filename"), "desc": param("Description"), "cwd": dirParam("Recorded working directory"), "tags": {Type: "array"}, "author": param("Author name"),
		}, positional: []string{"file"}, handler: a.importScript},
		{name: "export", description: "Write a saved script to a file or stdout without executing it", params: map[string]ochazuke.Parameter{"target": commandTarget(), "output": fileParam("Destination file, or - for stdout"), "append": {Type: "boolean"}}, positional: []string{"target", "output"}, handler: a.exportScript, output: "bytes"},
		{name: "duplicate", creates: true, description: "Copy a saved command's script and metadata under a new name", params: map[string]ochazuke.Parameter{"target": commandTarget(), "name": param("New name; duplicates receive a suffix")}, positional: []string{"target", "name"}, handler: a.duplicateScript},
		{name: "run-line", description: "Capture an unsaved shell line as a job; retain its script, input, stdout, stderr and exit status", params: map[string]ochazuke.Parameter{"line": required("Shell script text; not a saved command name"), "cwd": dirParam("Working directory; defaults to the invocation directory"), "stdin": fileParam("File to use as stdin"), "args": {Type: "array"}}, positional: []string{"line"}, handler: a.runLine, slot: "run", timeout: 15 * time.Minute},
	}
}

// readScript is deliberately reader-shaped: the shared source kind can replace
// file/stdin/command selection without changing the importing handler.
func (a *App) readScript(reader io.Reader, args ochazuke.Arguments, source, sourcePath string) (Command, error) {
	text, err := io.ReadAll(reader)
	if err != nil {
		return Command{}, err
	}
	author := stampAuthor(os.Getenv, "owner")
	if name := ochazuke.StringArg(args, "author", ""); name != "" {
		author.Name = name
	}
	name := ochazuke.StringArg(args, "name", "")
	if name == "" && sourcePath != "" {
		name = strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	}
	return a.store.Add(Add{Name: name, Description: ochazuke.StringArg(args, "desc", ""), Script: string(text), Cwd: ochazuke.StringArg(args, "cwd", ""), Tags: stringList(args["tags"]), Author: author, Source: source, SourcePath: sourcePath})
}

func (a *App) importScript(c ochazuke.Ctx) ochazuke.Result {
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		file := ochazuke.StringArg(c.Args, "file", "")
		command := ochazuke.StringArg(c.Args, "command", "")
		if file != "" && command != "" {
			return nil, errors.New("file and command are mutually exclusive")
		}
		if command != "" {
			r, err := a.captureLine(ctx, command, ochazuke.StringArg(c.Args, "cwd", ""), nil, "", "import")
			if err != nil {
				return nil, err
			}
			if r.ExitCode == nil || *r.ExitCode != 0 || r.StdoutCut {
				return nil, fmt.Errorf("source job %s: %s; output truncated=%t (job out/err retain its streams)", r.ID, runStatus(r.Run), r.StdoutCut)
			}
			data, err := runStream(r, "stdout")
			if err != nil {
				return nil, err
			}
			return a.readScript(bytes.NewReader(data), c.Args, "job:"+r.ID, "")
		}
		if file == "" || file == "-" {
			if a.tui {
				return nil, errors.New("stdin is unavailable in the TUI; supply a file or command")
			}
			return a.readScript(a.in, c.Args, "stdin", "")
		}
		path, err := filepath.Abs(file)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return a.readScript(f, c.Args, "file", path)
	}).Then(func(ochazuke.Ctx, any, error) { a.refresh() })
}

func writeStream(writer io.Writer, reader io.Reader) error {
	_, err := io.Copy(writer, reader)
	return err
}

func writeFileStream(path string, appendOutput bool, reader io.Reader) error {
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendOutput {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	return errors.Join(writeStream(f, reader), f.Close())
}

func (a *App) exportScript(c ochazuke.Ctx) ochazuke.Result {
	return ochazuke.Pending(func(context.Context) (any, error) {
		row, err := a.target(c)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(row.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		path := ochazuke.StringArg(c.Args, "output", "")
		if path == "" || path == "-" {
			b, err := io.ReadAll(f)
			return streamAnswer{Data: b}, err
		}
		appendOutput, _ := c.Args["append"].(bool)
		if err := differentFiles(row.Path, path); err != nil {
			return nil, err
		}
		return streamAnswer{}, writeFileStream(path, appendOutput, f)
	})
}

func differentFiles(source, destination string) error {
	from, err := os.Stat(source)
	if err != nil {
		return err
	}
	to, err := os.Stat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if os.SameFile(from, to) {
		return errors.New("source and destination name the same file")
	}
	return nil
}

func (a *App) duplicateScript(c ochazuke.Ctx) ochazuke.Result {
	return ochazuke.Pending(func(context.Context) (any, error) {
		row, err := a.target(c)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(row.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		args := ochazuke.Arguments{"name": ochazuke.StringArg(c.Args, "name", row.Name), "desc": row.Description, "cwd": row.Cwd, "tags": row.Tags}
		return a.readScript(f, args, "cmd:"+row.ID, row.SourcePath)
	}).Then(func(ochazuke.Ctx, any, error) { a.refresh() })
}

func (a *App) runLine(c ochazuke.Ctx) ochazuke.Result {
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		return a.captureLine(ctx, ochazuke.StringArg(c.Args, "line", ""), ochazuke.StringArg(c.Args, "cwd", ""), stringList(c.Args["args"]), ochazuke.StringArg(c.Args, "stdin", ""), "line")
	}).Then(func(ochazuke.Ctx, any, error) { a.refresh() })
}

func (a *App) captureLine(ctx context.Context, line, cwd string, args []string, inputPath, source string) (RunOutput, error) {
	id, err := token()
	if err != nil {
		return RunOutput{}, err
	}
	row := Command{Name: line, Cwd: cwd, Line: &line, JobID: time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + id}
	in, err := a.openInput(inputPath, "file")
	if err != nil {
		return RunOutput{}, err
	}
	defer in.close()
	recorder := *a
	recorder.out, recorder.errOut = io.Discard, io.Discard
	cmd := scriptCommand(ctx, row, args)
	if in != nil {
		cmd.Stdin = in.file
	}
	finish, err := recorder.prepareRun(cmd, row, args, source, in)
	if err != nil {
		return RunOutput{}, err
	}
	runErr := a.run(cmd)
	if err := finish(runErr); err != nil {
		return RunOutput{}, err
	}
	return a.store.ReadRun(row.JobID)
}

func runStream(r RunOutput, stream string) ([]byte, error) {
	for _, value := range []struct{ name, text, encoded string }{
		{"stdout", r.Stdout, r.StdoutBase64},
		{"stderr", r.Stderr, r.StderrBase64},
		{"stdin", r.Input, r.InputBase64},
	} {
		if value.name == stream {
			if value.encoded != "" {
				return base64.StdEncoding.DecodeString(value.encoded)
			}
			return []byte(value.text), nil
		}
	}
	return nil, fmt.Errorf("unknown stream %q", stream)
}

func (a *App) saveJob(c ochazuke.Ctx) ochazuke.Result {
	return ochazuke.Pending(func(context.Context) (any, error) {
		r, err := a.store.ReadRun(ochazuke.StringArg(c.Args, "job", ""))
		if err != nil {
			return nil, err
		}
		row, err := a.rerunCommand(r)
		if err != nil {
			return nil, err
		}
		var script string
		if row.Line != nil {
			script = *row.Line
		} else {
			script, err = a.store.Script(row)
			if err != nil {
				return nil, err
			}
		}
		args := ochazuke.Arguments{"name": ochazuke.StringArg(c.Args, "name", ""), "desc": ochazuke.StringArg(c.Args, "desc", ""), "cwd": r.Cwd}
		return a.readScript(strings.NewReader(script), args, "job:"+r.ID, "")
	}).Then(func(ochazuke.Ctx, any, error) { a.refresh() })
}

func (a *App) openJob(c ochazuke.Ctx) ochazuke.Result {
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		r, err := a.store.ReadRun(ochazuke.StringArg(c.Args, "job", ""))
		if err != nil {
			return nil, err
		}
		stream := ochazuke.StringArg(c.Args, "stream", "stdout")
		if err := validStream(stream); err != nil {
			return nil, err
		}
		path := a.store.runPath(r.Run, streamExtension(stream))
		if with := ochazuke.StringArg(c.Args, "with", ""); with != "" {
			return nil, a.r.Opener().OpenWith(with, path)
		}
		host, mounted, err := ocli.HostFromEnv()
		if err != nil {
			return nil, err
		}
		if mounted {
			_, err := host.Run(ctx, "ochazuke.opener.run", ochazuke.Arguments{"text": path})
			return nil, err
		}
		return path, nil
	}).Continue(func(value any, err error) (func(context.Context) (any, error), any, error) {
		if err != nil || value == nil {
			return nil, value, err
		}
		action, err := a.r.Open(value.(string))
		return nil, action, err
	})
}
