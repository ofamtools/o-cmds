package main

import (
	"path/filepath"
	"regexp"
	"strings"

	ocli "ofam.dev/ochazuke/cli"
)

// Detail styles of the standalone page (setting view.detail-style).
const (
	detailStyleScript   = "script"
	detailStyleMarkdown = "markdown"
)

// detailFields are the record fields a host shows when its own detail.sections
// setting is off (the framework's record view).
var detailFields = []string{"name", "description", "author", "cwd", "tags", "last_run", "script"}

// detailSections is the built-in, read-only document every catalog host
// renders without registering an o-cmds detail kind: metadata above the
// script as a fenced Markdown code block (row field detail_md).
var detailSections = []ocli.CatalogDetailSection{
	{Kind: "fields", Fields: []ocli.CatalogDetailField{
		{Label: "Name", Field: "name"},
		{Label: "Description", Field: "description"},
		{Label: "Author", Field: "author_label"},
		{Label: "Age", Field: "age"},
		{Label: "Last run", Field: "last_run"},
		{Label: "Cwd", Field: "cwd", Format: "path"},
		{Label: "Tags", Field: "tags", Format: "list"},
	}},
	{Kind: "markdown", Field: "detail_md"},
}

// interpreterLanguages maps a shebang interpreter (version digits removed) to
// the code block's info string; an unlisted interpreter names itself.
var interpreterLanguages = map[string]string{
	"python": "python", "node": "javascript", "nodejs": "javascript",
	"deno": "typescript", "bun": "javascript", "ruby": "ruby", "perl": "perl",
	"php": "php", "lua": "lua", "bash": "bash", "zsh": "zsh", "sh": "sh",
	"dash": "sh", "ksh": "sh", "fish": "fish", "awk": "awk", "gawk": "awk",
	"tclsh": "tcl", "osascript": "applescript", "pwsh": "powershell",
}

// extensionLanguages maps a script file extension to an info string.
var extensionLanguages = map[string]string{
	".sh": "sh", ".bash": "bash", ".zsh": "zsh", ".fish": "fish", ".py": "python",
	".js": "javascript", ".ts": "typescript", ".rb": "ruby", ".pl": "perl",
	".php": "php", ".lua": "lua", ".awk": "awk",
}

var versionSuffix = regexp.MustCompile(`[0-9.]+$`)
var infoWord = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*$`)

// scriptLanguage names a script's language from its shebang, else from the
// file extension, else sh (o-cmds runs scripts with sh).
func scriptLanguage(script, path string) string {
	if line, _, _ := strings.Cut(script, "\n"); strings.HasPrefix(line, "#!") {
		fields := strings.Fields(strings.TrimPrefix(line, "#!"))
		if len(fields) > 0 {
			interp := filepath.Base(fields[0])
			if interp == "env" {
				interp = ""
				for _, f := range fields[1:] {
					if !strings.HasPrefix(f, "-") && !strings.Contains(f, "=") {
						interp = filepath.Base(f)
						break
					}
				}
			}
			interp = versionSuffix.ReplaceAllString(strings.ToLower(interp), "")
			if lang, ok := interpreterLanguages[interp]; ok {
				return lang
			}
			if infoWord.MatchString(interp) {
				return interp
			}
		}
	}
	if lang, ok := extensionLanguages[strings.ToLower(filepath.Ext(path))]; ok {
		return lang
	}
	return "sh"
}

// scriptMarkdown fences a script verbatim: blank lines, indentation and tabs
// are kept, and the fence is longer than any backtick run inside the script.
func scriptMarkdown(script, path string) string {
	longest, run := 0, 0
	for _, r := range script {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	body := strings.TrimSuffix(script, "\n")
	return fence + scriptLanguage(script, path) + "\n" + body + "\n" + fence + "\n"
}
