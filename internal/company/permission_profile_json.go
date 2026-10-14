package company

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodePermissionJSONObject shares the editor's duplicate/case-sensitive key
// boundary for profile commands without inventing a second policy decoder.
func DecodePermissionJSONObject(r io.Reader, out any, allowed, required []string) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err = rejectDuplicatePermissionKeys(d); err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		return fmt.Errorf("exactly one JSON object required")
	}
	if err = permissionObjectKeys(raw, allowed...); err != nil {
		return err
	}
	if err = permissionRequiredKeys(raw, required...); err != nil {
		return err
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	if obj == nil {
		return fmt.Errorf("command object required")
	}
	return json.Unmarshal(raw, out)
}
