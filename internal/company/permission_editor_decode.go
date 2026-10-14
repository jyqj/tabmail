package company

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodePermissionEditorCommand preserves absent patch fields versus null intent.
func DecodePermissionEditorCommand(r io.Reader) (PermissionEditorCommand, error) {
	var cmd PermissionEditorCommand
	raw, err := permissionCommandJSON(r, false)
	if err != nil {
		return cmd, err
	}
	if err = json.Unmarshal(raw, &cmd); err != nil {
		return PermissionEditorCommand{}, err
	}
	if err = cmd.ExpectedRevision.Validate(); err != nil {
		return PermissionEditorCommand{}, err
	}
	if err = cmd.Patch.Validate(); err != nil {
		return PermissionEditorCommand{}, err
	}
	return cmd, nil
}

// DecodePermissionAssignmentCommand requires explicit assignment intent. Missing
// profile keys can never silently become an unbind command.
func DecodePermissionAssignmentCommand(r io.Reader) (PermissionAssignmentCommand, error) {
	var cmd PermissionAssignmentCommand
	raw, err := permissionCommandJSON(r, true)
	if err != nil {
		return cmd, err
	}
	if err = json.Unmarshal(raw, &cmd); err != nil {
		return PermissionAssignmentCommand{}, err
	}
	if err = cmd.ExpectedRevision.Validate(); err != nil {
		return PermissionAssignmentCommand{}, err
	}
	if err = cmd.Patch.Validate(); err != nil {
		return PermissionAssignmentCommand{}, err
	}
	if (cmd.ProfileID == nil) != (cmd.ProfileRevision == nil) {
		return PermissionAssignmentCommand{}, fmt.Errorf("selected profile identity and revision must be supplied together")
	}
	if cmd.ProfileID != nil {
		observed := PermissionRevision{UserID: cmd.ExpectedRevision.UserID, TenantID: cmd.ExpectedRevision.TenantID, UserRevision: cmd.ExpectedRevision.UserRevision, ProfileID: cmd.ProfileID, ProfileRevision: cmd.ProfileRevision}
		if err = observed.Validate(); err != nil {
			return PermissionAssignmentCommand{}, err
		}
	}
	return cmd, nil
}

func permissionCommandJSON(r io.Reader, assignment bool) ([]byte, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	scan.UseNumber()
	if err = rejectDuplicatePermissionKeys(scan); err != nil {
		return nil, err
	}
	if _, err = scan.Token(); err != io.EOF {
		return nil, fmt.Errorf("exactly one JSON object required")
	}
	keys := []string{"expected_revision", "patch"}
	if assignment {
		keys = append(keys, "profile_id", "profile_revision")
	}
	if err = permissionObjectKeys(raw, keys...); err != nil {
		return nil, err
	}
	if err = permissionRequiredKeys(raw, keys...); err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	revisionKeys := []string{"user_id", "tenant_id", "user_revision", "profile_id", "profile_revision"}
	if err = permissionObjectKeys(envelope["expected_revision"], revisionKeys...); err != nil {
		return nil, err
	}
	if err = permissionRequiredKeys(envelope["expected_revision"], revisionKeys...); err != nil {
		return nil, err
	}
	if err = permissionObjectKeys(envelope["patch"], "can_send", "daily_send_quota", "daily_receive_quota", "max_mailboxes", "max_domains", "can_create_domains", "can_create_routes", "can_create_api_keys", "domain_access"); err != nil {
		return nil, err
	}
	var patch map[string]json.RawMessage
	if err = json.Unmarshal(envelope["patch"], &patch); err != nil {
		return nil, err
	}
	if domain, ok := patch["domain_access"]; ok && !bytes.Equal(bytes.TrimSpace(domain), []byte("null")) {
		if err = permissionObjectKeys(domain, "mode", "zone_ids"); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

func permissionRequiredKeys(raw []byte, keys ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return err
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("required JSON field %q", key)
		}
	}
	return nil
}

// Reject duplicate keys at every nesting level, rather than letting a second
// value silently replace earlier intent (including null/false and revisions).
func rejectDuplicatePermissionKeys(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err = d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("JSON object key required")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = true
			if err = rejectDuplicatePermissionKeys(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = rejectDuplicatePermissionKeys(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func permissionObjectKeys(raw []byte, allowed ...string) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	if values == nil {
		return fmt.Errorf("JSON object required")
	}
	for key := range values {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown JSON field %q", key)
		}
	}
	return nil
}
