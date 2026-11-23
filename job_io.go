package main

import (
	"encoding/base64"
	"fmt"
)

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

// runStream returns a captured stream's exact bytes (binary streams are
// kept base64-encoded beside their lossy text).
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
