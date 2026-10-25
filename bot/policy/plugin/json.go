package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const maxJSONDepth = 64

// Strict decoding rejects ambiguous object members before decoding into a map
// could overwrite them. The same boundary serves configs, schemas and results.
func strictJSON(data []byte, dst any) error {
	scan := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSON(scan, 0); err != nil {
		return err
	}
	if _, err := scan.Token(); err != io.EOF {
		return fmt.Errorf("expected a single JSON value")
	}
	decode := json.NewDecoder(bytes.NewReader(data))
	decode.DisallowUnknownFields()
	return decode.Decode(dst)
}

func scanJSON(d *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("JSON exceeds %d nesting levels", maxJSONDepth)
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("expected object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if err := scanJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := scanJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	_, err = d.Token()
	return err
}
