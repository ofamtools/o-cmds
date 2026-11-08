package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// The MCP surface is an explicit allowlist over the app's verb descriptions.
// Execution is deliberately absent from both discovery and dispatch.
func (a *App) tools() []mcpTool {
	allowed := map[string][]string{"cmd.add": {"name", "desc", "script", "cwd", "tags", "author"}, "cmd.list": {"filter"}, "cmd.show": {"target"}, "cmd.dismiss": {"target"}, "cmd.undismiss": {"target"}, "job.list": {"command"}, "job.show": {"job"}}
	out := []mcpTool{}
	for _, spec := range a.verbs() {
		names, ok := allowed[spec.noun+"."+spec.name]
		if !ok {
			continue
		}
		props := map[string]any{}
		for _, name := range names {
			p := spec.params[name]
			field := map[string]any{"type": p.Type, "description": p.Description}
			if p.Type == "array" {
				field["items"] = map[string]string{"type": "string"}
			}
			props[name] = field
		}
		schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if spec.name == "add" {
			schema["required"] = []string{"script"}
		}
		if spec.name == "show" || spec.name == "dismiss" || spec.name == "undismiss" {
			schema["required"] = names
		}
		out = append(out, mcpTool{Name: spec.noun + "_" + spec.name, Description: spec.description, InputSchema: schema})
	}
	return out
}

func toolContent(v any, err error) any {
	text := ""
	if err != nil {
		text = err.Error()
	} else {
		b, e := json.Marshal(v)
		if e != nil {
			text = e.Error()
			err = e
		} else {
			text = string(b)
		}
	}
	return map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": err != nil}
}

func (a *App) callTool(name string, raw json.RawMessage, client string) any {
	// Decode typed arguments rather than accepting a schema we cannot enforce.
	decode := func(v any) error {
		if len(raw) == 0 {
			raw = []byte("{}")
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		return d.Decode(v)
	}
	switch name {
	case "job_list":
		var p struct {
			Command string `json:"command"`
		}
		if e := decode(&p); e != nil {
			return toolContent(nil, e)
		}
		id := ""
		if p.Command != "" {
			c, e := a.store.Resolve(p.Command, a.newestFirst())
			if e != nil {
				return toolContent(nil, e)
			}
			id = c.ID
		}
		rows, e := a.runRows(id)
		return toolContent(rows, e)
	case "job_show":
		var p struct {
			Job string `json:"job"`
		}
		if e := decode(&p); e != nil {
			return toolContent(nil, e)
		}
		r, e := a.store.ReadRun(p.Job)
		if len(r.Input) > 64*1024 || len(r.InputBase64) > 64*1024 {
			r.Input = ""
			r.InputBase64 = ""
		}
		return toolContent(r, e)
	case "cmd_add":
		var p struct {
			Name   string   `json:"name"`
			Desc   string   `json:"desc"`
			Script *string  `json:"script"`
			Cwd    string   `json:"cwd"`
			Tags   []string `json:"tags"`
			Author string   `json:"author"`
		}
		if e := decode(&p); e != nil {
			return toolContent(nil, e)
		}
		if p.Script == nil {
			return toolContent(nil, fmt.Errorf("script is required; an empty script is allowed"))
		}
		author := stampAuthor(os.Getenv, client)
		if p.Author != "" {
			author.Name = p.Author
		}
		if author.Harness == "" {
			author.Harness = client
		}
		c, e := a.store.Add(Add{Name: p.Name, Description: p.Desc, Script: *p.Script, Cwd: p.Cwd, Tags: p.Tags, Author: author})
		return toolContent(c, e)
	case "cmd_list":
		var p struct {
			Filter string `json:"filter"`
		}
		if e := decode(&p); e != nil {
			return toolContent(nil, e)
		}
		rows, e := a.rows(p.Filter)
		return toolContent(rows, e)
	case "cmd_dismiss", "cmd_undismiss":
		var p struct {
			Target string `json:"target"`
		}
		if e := decode(&p); e != nil {
			return toolContent(nil, e)
		}
		c, e := a.store.Resolve(p.Target, a.newestFirst())
		if e != nil {
			return toolContent(nil, e)
		}
		dismissed := name == "cmd_dismiss"
		v, e := a.store.ChangeAll([]string{"cmd/" + c.ID}, func(v *Command, _ []Command) { v.Dismissed = dismissed })
		return toolContent(v, e)
	case "cmd_show":
		var p struct {
			Target string `json:"target"`
		}
		if e := decode(&p); e != nil {
			return toolContent(nil, e)
		}
		c, e := a.store.Resolve(p.Target, a.newestFirst())
		if e != nil {
			return toolContent(nil, e)
		}
		script, e := a.store.Script(c)
		return toolContent(struct {
			Command
			Script string `json:"script"`
		}{c, script}, e)
	default:
		return toolContent(nil, fmt.Errorf("unknown tool %q", name))
	}
}

// Stdio framing and the initialize/tools lifecycle follow the family's tuios
// MCP server: newline-delimited JSON-RPC, notifications receive no response.
func (a *App) mcp() error {
	scan := bufio.NewScanner(a.in)
	scan.Buffer(make([]byte, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(a.out)
	client := "mcp"
	for scan.Scan() {
		line := scan.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var q rpcRequest
		resp := rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null")}
		if e := json.Unmarshal(line, &q); e != nil {
			resp.Error = &rpcError{-32700, "parse error"}
		} else {
			if len(q.ID) > 0 {
				resp.ID = q.ID
			}
			if q.JSONRPC != "2.0" || q.Method == "" {
				resp.Error = &rpcError{-32600, "invalid request"}
			} else if len(q.ID) == 0 {
				continue
			} else {
				switch q.Method {
				case "initialize":
					var p struct {
						Protocol string `json:"protocolVersion"`
						Client   struct {
							Name string `json:"name"`
						} `json:"clientInfo"`
					}
					if e := json.Unmarshal(q.Params, &p); e != nil {
						resp.Error = &rpcError{-32602, "invalid initialize parameters"}
						break
					}
					protocol := "2025-11-25"
					for _, v := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"} {
						if p.Protocol == v {
							protocol = v
						}
					}
					if p.Client.Name != "" {
						client = p.Client.Name
					}
					resp.Result = map[string]any{"protocolVersion": protocol, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "o-cmds", "version": version}}
				case "ping":
					resp.Result = map[string]any{}
				case "tools/list":
					resp.Result = map[string]any{"tools": a.tools()}
				case "tools/call":
					var p struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}
					if e := json.Unmarshal(q.Params, &p); e != nil || p.Name == "" {
						resp.Error = &rpcError{-32602, "invalid tool parameters"}
					} else {
						resp.Result = a.callTool(p.Name, p.Arguments, client)
					}
				default:
					resp.Error = &rpcError{-32601, "method not found"}
				}
			}
		}
		if e := enc.Encode(resp); e != nil {
			return e
		}
	}
	return scan.Err()
}
