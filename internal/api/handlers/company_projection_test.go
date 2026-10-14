package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"tabmail/internal/company"
)

func TestCompanyWireCollectionsAreArraysWithoutMutatingSource(t *testing.T) {
	draft := company.Draft{TemplateVersion: &company.DraftTemplateVersion{Snapshot: &company.TemplateDraft{}}}
	template := company.Template{}
	version := company.TemplateVersion{}
	cases := []struct {
		name  string
		value any
		path  string
	}{
		{"draft-pointer", &draft, "data.payload.to"},
		{"draft-value", draft, "data.payload.to"},
		{"draft-list", []company.Draft{draft}, "data.0.payload.to"},
		{"draft-pinned-template", &draft, "data.template_version.snapshot.variables"},
		{"template-pointer", &template, "data.draft.variables"},
		{"template-value", template, "data.draft.variables"},
		{"template-list", []company.Template{template}, "data.0.draft.variables"},
		{"version-pointer", &version, "data.snapshot.variables"},
		{"version-value", version, "data.snapshot.variables"},
		{"version-list", []company.TemplateVersion{version}, "data.0.snapshot.variables"},
		{"compose-payload", &company.DraftPayload{}, "data.to"},
		{"compose-payload-value", company.DraftPayload{}, "data.to"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			writeJSON(w, 200, envelope{Data: tc.value})
			var data any
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			for _, key := range strings.Split(tc.path, ".") {
				if key == "0" {
					list, ok := data.([]any)
					if !ok || len(list) != 1 {
						t.Fatalf("invalid list at %s", tc.path)
					}
					data = list[0]
				} else {
					object, ok := data.(map[string]any)
					if !ok {
						t.Fatalf("invalid object at %s", tc.path)
					}
					data = object[key]
				}
			}
			if _, ok := data.([]any); !ok {
				t.Errorf("%s is %T, expected an array", tc.path, data)
			}
			after, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("HTTP projection mutated a source used for persistence/digests")
			}
		})
	}
}

func TestCompanyWireProjectionPreservesDomainSerializationAndNullableObjects(t *testing.T) {
	// Published template digests and draft creation receipts already depend on
	// this exact encoding. Only transport output may normalize nil collections.
	value := company.TemplateDraft{}
	before := company.Digest(value)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"variables":null`) {
		t.Fatal("domain serialization changed")
	}
	w := httptest.NewRecorder()
	writeJSON(w, 200, envelope{Data: &company.Template{Draft: value}})
	if company.Digest(value) != before {
		t.Fatal("transport changed template hash")
	}
	var settings *company.Settings
	w = httptest.NewRecorder()
	writeJSON(w, 200, envelope{Data: settings})
	if strings.TrimSpace(w.Body.String()) != `{"data":null}` {
		t.Fatalf("unconfigured settings changed: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	writeJSON(w, 200, envelope{Data: map[string]any{"to": nil}})
	if strings.TrimSpace(w.Body.String()) != `{"data":{"to":null}}` {
		t.Fatal("unrelated response shape changed")
	}
}
