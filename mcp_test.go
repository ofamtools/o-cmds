package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPToolsLifecycleAndNoRun(t *testing.T) {
	s := fixture(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"broker"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cmd_add","arguments":{"name":"agent gift","script":"echo gift\n","tags":["demo"]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"cmd_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"cmd_show","arguments":{"target":"1"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"cmd_run","arguments":{"target":"1"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	}, "\n") + "\n"
	var out, errOut bytes.Buffer
	a, e := newApp(s, strings.NewReader(input), &out, &errOut)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.mcp(); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 7 {
		t.Fatalf("responses=%s", out.String())
	}
	var tools struct {
		Result struct {
			Tools []mcpTool `json:"tools"`
		} `json:"result"`
	}
	if e = json.Unmarshal([]byte(lines[1]), &tools); e != nil {
		t.Fatal(e)
	}
	names := map[string]bool{}
	for _, tool := range tools.Result.Tools {
		names[tool.Name] = true
	}
	if len(names) != 3 || !names["cmd_add"] || !names["cmd_list"] || !names["cmd_show"] {
		t.Fatalf("tools=%v", names)
	}
	var call struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if e = json.Unmarshal([]byte(lines[5]), &call); e != nil || !call.Result.IsError {
		t.Fatalf("cmd_run=%s %v", lines[5], e)
	}
	if !strings.Contains(lines[4], "echo gift") {
		t.Fatalf("show=%s", lines[4])
	}
	cs, e := s.List()
	if e != nil || len(cs) != 1 || cs[0].Author.Name != "broker" || cs[0].Author.Harness != "broker" {
		t.Fatalf("author=%+v %v", cs, e)
	}
}

func TestMCPMalformedAndEmptyScript(t *testing.T) {
	s := fixture(t)
	var out bytes.Buffer
	a, e := newApp(s, strings.NewReader("invalid\n{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"missing\"}\n"), &out, &out)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.mcp(); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "-32700") || !strings.Contains(out.String(), "-32601") {
		t.Fatalf("errors=%s", out.String())
	}
	result := a.callTool("cmd_add", json.RawMessage(`{"script":""}`), "tester")
	b, _ := json.Marshal(result)
	if strings.Contains(string(b), `"isError":true`) {
		t.Fatalf("empty refused: %s", b)
	}
	result = a.callTool("cmd_add", json.RawMessage(`{}`), "tester")
	b, _ = json.Marshal(result)
	if !strings.Contains(string(b), `"isError":true`) {
		t.Fatalf("missing script accepted: %s", b)
	}
}
