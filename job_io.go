package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

type streamAnswer struct {
	Data []byte `json:"data"`
}

func jobTarget() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Noun: "job", From: "row:id", Description: "Job ID, prefix or displayed number"}
}

func streamParam() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Enum: []string{"stdout", "stderr", "stdin"}, Description: "Captured stream (default stdout)"}
}

func (a *App) jobFileSpecs() []verbSpec {
	return []verbSpec{
		{name: "save", description: "Save the script behind a job as a new named command (unsaved lines keep their exact script)", params: map[string]ochazuke.Parameter{"job": jobTarget(), "name": required("Name of the saved script"), "desc": param("Description")}, positional: []string{"job", "name"}, handler: a.saveJob},
		{name: "write", description: "Send captured output, errors or input to a file, stdout, clipboard or register", params: map[string]ochazuke.Parameter{
			"job": jobTarget(), "stream": streamParam(), "output": fileParam("Destination file, or - for stdout"), "append": {Type: "boolean", Description: "Append to a file or named register"}, "clipboard": {Type: "boolean"}, "register": {Type: "string", Noun: "ochazuke.register", Free: true, Description: "Destination register"},
		}, positional: []string{"job", "output"}, handler: a.writeJob, output: "bytes"},
		{name: "yank", description: "Copy a job's captured stream to the clipboard or a named register", params: map[string]ochazuke.Parameter{"job": jobTarget(), "stream": streamParam(), "register": {Type: "string", Noun: "ochazuke.register", Free: true}, "append": {Type: "boolean"}}, positional: []string{"job"}, handler: func(c ochazuke.Ctx) ochazuke.Result {
			if ochazuke.StringArg(c.Args, "register", "") == "" {
				args := ochazuke.Arguments{}
				for key, value := range c.Args {
					args[key] = value
				}
				args["clipboard"] = true
				c.Args = args
			}
			return a.writeJob(c)
		}, output: "bytes"},
		{name: "open", description: "Open a captured stream through the generic opener, never execute it", params: map[string]ochazuke.Parameter{"job": jobTarget(), "stream": streamParam(), "with": param("Optional application name")}, positional: []string{"job"}, handler: a.openJob, agent: ochazuke.AgentConfirm},
	}
}

func registerRequest(name, text string, appendOutput bool) ochazuke.Request {
	if appendOutput {
		return ochazuke.Request{Noun: "ochazuke.register", Verb: "append", Args: ochazuke.Arguments{"name": name, "text": text, "shape": "char"}}
	}
	return ochazuke.Request{Noun: "ochazuke.register", Verb: "set", Args: ochazuke.Arguments{"name": name, "props": map[string]any{"text": text, "shape": "char"}}}
}

func (a *App) writeJob(c ochazuke.Ctx) ochazuke.Result {
	register := ochazuke.StringArg(c.Args, "register", "")
	path := ochazuke.StringArg(c.Args, "output", "")
	clip, _ := c.Args["clipboard"].(bool)
	appendOutput, _ := c.Args["append"].(bool)
	sinks := 0
	for _, selected := range []bool{register != "", path != "", clip} {
		if selected {
			sinks++
		}
	}
	if sinks > 1 {
		return ochazuke.Failed(errors.New("choose only one destination: output, clipboard or register"))
	}
	if appendOutput && (clip || (path == "" && register == "") || path == "-") {
		return ochazuke.Failed(errors.New("append needs a file or named register"))
	}
	if register != "" && (len(register) != 1 || !((register[0] >= 'a' && register[0] <= 'z') || (register[0] >= 'A' && register[0] <= 'Z'))) {
		return ochazuke.Failed(errors.New("output register must be a named register a-z (uppercase aliases allowed); use clipboard for the system clipboard"))
	}
	localRegister := false
	return ochazuke.Pending(func(ctx context.Context) (any, error) {
		r, err := a.store.ReadRun(ochazuke.StringArg(c.Args, "job", ""))
		if err != nil {
			return nil, err
		}
		stream := ochazuke.StringArg(c.Args, "stream", "stdout")
		data, err := runStream(r, stream)
		if err != nil {
			return nil, err
		}
		if (clip || register != "") && !utf8.Valid(data) {
			return nil, errors.New("clipboard and registers require UTF-8 text; use output for binary bytes")
		}
		if clip {
			return streamAnswer{}, clipboard(ctx, string(data), a.run, a.errOut)
		}
		if register != "" {
			host, mounted, err := ocli.HostFromEnv()
			if err != nil {
				return nil, err
			}
			if mounted {
				request := registerRequest(register, string(data), appendOutput)
				_, err := host.Call(ctx, request.Noun, request.Verb, request.Args)
				return streamAnswer{}, err
			}
			localRegister = true
			return streamAnswer{Data: data}, nil
		}
		if path == "" || path == "-" {
			return streamAnswer{Data: data}, nil
		}
		if err := differentFiles(a.store.runPath(r.Run, streamExtension(stream)), path); err != nil {
			return nil, err
		}
		return streamAnswer{}, writeFileStream(path, appendOutput, bytes.NewReader(data))
	}).Continue(func(value any, err error) (func(context.Context) (any, error), any, error) {
		if err != nil || !localRegister {
			return nil, value, err
		}
		_, err = a.r.ExecuteAt(c.Context, registerRequest(register, string(value.(streamAnswer).Data), appendOutput))
		return nil, streamAnswer{}, err
	})
}

func streamExtension(name string) string {
	for _, stream := range []struct{ name, extension string }{{"stdout", ".out"}, {"stderr", ".err"}, {"stdin", ".in"}} {
		if stream.name == name {
			return stream.extension
		}
	}
	return ""
}

func validStream(name string) error {
	if streamExtension(name) == "" {
		return fmt.Errorf("unknown captured stream %q", name)
	}
	return nil
}
