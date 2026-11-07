package fileicons

import (
	"bytes"
	"encoding/json"
)

// unmarshalJSONC decodes JSON with comments and trailing commas, as VS
// Code reads extensions' files, into v.
func unmarshalJSONC(data []byte, v any) error {
	return json.Unmarshal(stripJSONC(data), v)
}

// stripJSONC returns data without a byte order mark, comments (// and
// /* */) and commas before a closing bracket or brace, outside strings.
func stripJSONC(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	out := make([]byte, 0, len(data))
	// comma is where in out a comma waits to be kept or dropped, or -1.
	comma := -1
	for i := 0; i < len(data); i++ {
		ch := data[i]
		switch {
		case ch == '"':
			comma = -1
			j := i + 1
			for j < len(data) && data[j] != '"' {
				if data[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(data))
			out = append(out, data[i:j]...)
			i = j - 1
		case ch == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i--
		case ch == '/' && i+1 < len(data) && data[i+1] == '*':
			end := bytes.Index(data[i+2:], []byte("*/"))
			if end < 0 {
				i = len(data)
			} else {
				i += 2 + end + 1
			}
		case ch == ',':
			comma = len(out)
			out = append(out, ch)
		case ch == '}' || ch == ']':
			if comma >= 0 {
				out = append(out[:comma], out[comma+1:]...)
			}
			comma = -1
			out = append(out, ch)
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			out = append(out, ch)
		default:
			comma = -1
			out = append(out, ch)
		}
	}
	return out
}
