package llm

import (
	"encoding/json"
	"io"
	"os"
)

// decodeJSON decodes r into dst using json.NewDecoder. Used by
// the test server handlers to parse incoming request bodies.
func decodeJSON(r io.Reader, dst any) error {
	return json.NewDecoder(r).Decode(dst)
}

// writeFile is a thin alias for os.WriteFile, exposed in the
// test package so individual tests don't need to import "os".
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
