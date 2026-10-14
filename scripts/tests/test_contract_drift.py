import importlib.util
import sys
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('ccd', Path(__file__).resolve().parents[1] / 'check_contract_drift.py')
ccd = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = ccd
spec.loader.exec_module(ccd)

GO_LEGAL = '''
package dto

type Status string

type Child struct {
	X int `json:"x"`
}

type Widget struct {
	ID         uuid.UUID         `json:"id"`
	Name       string            `json:"name"`
	Count      int               `json:"count"`
	When       time.Time         `json:"when"`
	WhenOrNull *time.Time        `json:"when_or_null"`
	Nullable   int               `json:"nullable"`
	OptCount   *int              `json:"opt_count,omitempty"`
	Tags       []string          `json:"tags"`
	OptTags    []uuid.UUID       `json:"opt_tags,omitempty"`
	Child      *Child            `json:"child,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	Raw        json.RawMessage   `json:"raw,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	State      Status            `json:"state"`
}
'''

TS_LEGAL = '''
export type Status = "on" | "off";
export interface Child {
  x: number;
}
export interface Widget {
  id: string;
  name: string;
  count: number;
  when: string;
  when_or_null: string | null;
  nullable: number;
  opt_count?: number | null;
  tags: string[];
  opt_tags?: string[];
  child?: Child;
  meta?: Record<string, string>;
  raw?: unknown;
  kind?: "a" | "b";
  state: Status;
}
'''

PAIRS = {"Widget": "Widget", "Child": "Child"}
NESTED = {"Widget": "Widget", "Child": "Child"}


def run_pairs(go_texts, ts_texts, pairs=PAIRS, nested=NESTED):
    return ccd.check_pairs(pairs, go_texts, ts_texts, nested)


def subst(text, old, new):
    assert old in text, old
    return text.replace(old, new)


class LegalContractTests(unittest.TestCase):
    def test_legal_contract_passes(self):
        self.assertEqual(run_pairs([GO_LEGAL], [TS_LEGAL]), [])

    def test_inline_object_for_unmapped_nested_struct_passes(self):
        go = subst(GO_LEGAL, 'Child      *Child            `json:"child,omitempty"`',
                   'Stats      Stats             `json:"stats,omitempty"`')
        ts = subst(TS_LEGAL, 'child?: Child;', 'stats?: { total: number };')
        self.assertEqual(run_pairs([go], [ts], {"Widget": "Widget"}, {"Widget": "Widget"}), [])


class FieldSetTests(unittest.TestCase):
    def test_renamed_go_field_fails(self):
        go = subst(GO_LEGAL, 'Name       string            `json:"name"`',
                   'Title      string            `json:"title"`')
        errors = run_pairs([go], [TS_LEGAL])
        self.assertTrue(any(e.startswith('[drift] Widget->Widget') for e in errors), errors)

    def test_renamed_ts_field_fails(self):
        ts = subst(TS_LEGAL, 'name: string;', 'title: string;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[drift] Widget->Widget') for e in errors), errors)

    def test_deleted_go_model_fails(self):
        errors = run_pairs([GO_LEGAL], [TS_LEGAL], pairs={"Missing": "Widget"})
        self.assertIn('[missing-go] Missing', errors)

    def test_deleted_ts_model_fails(self):
        errors = run_pairs([GO_LEGAL], [TS_LEGAL], pairs={"Widget": "Missing"})
        self.assertIn('[missing-ts] Missing', errors)


class DuplicateDefinitionTests(unittest.TestCase):
    def test_duplicate_go_struct_across_files_fails(self):
        errors = run_pairs([GO_LEGAL, GO_LEGAL], [TS_LEGAL])
        self.assertIn('[duplicate-go] Child', errors)
        self.assertIn('[duplicate-go] Widget', errors)

    def test_duplicate_ts_interface_across_files_fails(self):
        errors = run_pairs([GO_LEGAL], [TS_LEGAL, TS_LEGAL])
        self.assertIn('[duplicate-ts] Child', errors)
        self.assertIn('[duplicate-ts] Widget', errors)

    def test_duplicate_go_field_in_one_struct_fails(self):
        go = subst(GO_LEGAL, 'Count      int               `json:"count"`',
                   'Count      int               `json:"count"`\n\tCount2     int               `json:"count"`')
        errors = run_pairs([go], [TS_LEGAL])
        self.assertIn('[duplicate-go-field] Widget.count', errors)

    def test_duplicate_ts_field_in_one_interface_fails(self):
        ts = subst(TS_LEGAL, 'count: number;', 'count: number;\n  count: number;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertIn('[duplicate-ts-field] Widget.count', errors)


class NullabilityTests(unittest.TestCase):
    def test_pointer_without_omitempty_requires_ts_null(self):
        ts = subst(TS_LEGAL, 'when_or_null: string | null;', 'when_or_null: string;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[missing-null] Widget->Widget.when_or_null') for e in errors), errors)

    def test_value_field_with_ts_null_fails(self):
        ts = subst(TS_LEGAL, 'nullable: number;', 'nullable: number | null;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[null-drift] Widget->Widget.nullable') for e in errors), errors)

    def test_omitempty_value_with_ts_null_fails(self):
        ts = subst(TS_LEGAL, 'kind?: "a" | "b";', 'kind?: "a" | "b" | null;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[null-drift] Widget->Widget.kind') for e in errors), errors)

    def test_pointer_omitempty_must_be_optional_or_nullable(self):
        ts = subst(TS_LEGAL, 'opt_count?: number | null;', 'opt_count: number;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[optional-drift] Widget->Widget.opt_count') for e in errors), errors)


class ProjectionTests(unittest.TestCase):
    def test_scalar_type_change_fails(self):
        ts = subst(TS_LEGAL, 'count: number;', 'count: string;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[type-drift] Widget->Widget.count') for e in errors), errors)

    def test_uuid_projected_as_string_change_fails(self):
        ts = subst(TS_LEGAL, 'id: string;', 'id: number;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[type-drift] Widget->Widget.id') for e in errors), errors)

    def test_bool_projection_change_fails(self):
        go = subst(GO_LEGAL, 'Count      int               `json:"count"`',
                   'Count      bool              `json:"count"`')
        errors = run_pairs([go], [TS_LEGAL])
        self.assertTrue(any(e.startswith('[type-drift] Widget->Widget.count') for e in errors), errors)

    def test_slice_without_ts_array_fails(self):
        ts = subst(TS_LEGAL, 'tags: string[];', 'tags: string;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[array-drift] Widget->Widget.tags') for e in errors), errors)

    def test_slice_element_type_drift_fails(self):
        ts = subst(TS_LEGAL, 'opt_tags?: string[];', 'opt_tags?: number[];')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[type-drift] Widget->Widget.opt_tags') for e in errors), errors)

    def test_named_string_type_against_number_alias_fails(self):
        ts = subst(TS_LEGAL, 'export type Status = "on" | "off";', 'export type Status = number;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[type-drift] Widget->Widget.state') for e in errors), errors)

    def test_unresolvable_alias_fails(self):
        ts = subst(TS_LEGAL, 'state: Status;', 'state: UnknownStatus;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[type-drift] Widget->Widget.state') for e in errors), errors)


class NestedReferenceTests(unittest.TestCase):
    def test_mapped_nested_struct_with_wrong_ts_name_fails(self):
        ts = subst(TS_LEGAL, 'child?: Child;', 'child?: Widget;')
        errors = run_pairs([GO_LEGAL], [ts])
        self.assertTrue(any(e.startswith('[nested-drift] Widget->Widget.child') for e in errors), errors)


class SharedEntryTests(unittest.TestCase):
    def test_shared_missing_go_is_reported(self):
        errors = ccd.check_shared(GO_LEGAL, TS_LEGAL)
        self.assertIn('[missing-go] Plan', errors)


class StrictWireContractTests(unittest.TestCase):
    def test_nullable_does_not_allow_an_omitted_property(self):
        ts = subst(TS_LEGAL, 'opt_count?: number | null;', 'opt_count: number | null;')
        self.assertTrue(any(e.startswith('[optional-drift]') for e in run_pairs([GO_LEGAL], [ts])))

    def test_omitted_value_type_must_also_be_optional(self):
        ts = subst(TS_LEGAL, 'kind?: "a" | "b";', 'kind: "a" | "b";')
        self.assertTrue(any(e.startswith('[optional-drift]') for e in run_pairs([GO_LEGAL], [ts])))

    def test_null_alone_cannot_represent_pointer_value(self):
        ts = subst(TS_LEGAL, 'when_or_null: string | null;', 'when_or_null: null;')
        self.assertTrue(any(e.startswith('[type-drift]') for e in run_pairs([GO_LEGAL], [ts])))

    def test_array_union_cannot_hide_a_non_array_member(self):
        ts = subst(TS_LEGAL, 'tags: string[];', 'tags: string[] | number;')
        self.assertTrue(any(e.startswith('[array-drift]') for e in run_pairs([GO_LEGAL], [ts])))

    def test_mapped_child_cannot_hide_unchecked_inline_shape(self):
        ts = subst(TS_LEGAL, 'child?: Child;', 'child?: { wrong: number };')
        self.assertTrue(any(e.startswith('[nested-drift]') for e in run_pairs([GO_LEGAL], [ts])))

    def test_union_split_preserves_nested_and_quoted_pipes(self):
        value = 'Array<{ name: string | null }> | "a|b" | null'
        self.assertEqual(ccd.ts_members(value), ['Array<{ name: string | null }>', '"a|b"', 'null'])

    def test_multiline_union_with_leading_pipe_is_legal(self):
        ts = subst(TS_LEGAL, 'export type Status = "on" | "off";', 'export type Status =\n  | "on"\n  | "off";')
        self.assertEqual(run_pairs([GO_LEGAL], [ts]), [])

    def test_unbalanced_type_fails_closed(self):
        with self.assertRaises(ValueError):
            ccd.ts_members('Array<string | null')


OPENAPI_LEGAL = {
    "openapi": "3.1.0",
    "components": {
        "schemas": {
            "Child": {
                "type": "object",
                "required": ["x"],
                "properties": {"x": {"type": "integer"}},
            },
            "Widget": {
                "type": "object",
                "required": [
                    "id", "name", "count", "when", "when_or_null",
                    "nullable", "tags", "state",
                ],
                "properties": {
                    "id": {"type": "string", "format": "uuid"},
                    "name": {"type": "string"},
                    "count": {"type": "integer"},
                    "when": {"type": "string", "format": "date-time"},
                    "when_or_null": {
                        "type": ["string", "null"],
                        "format": "date-time",
                    },
                    "nullable": {"type": "integer"},
                    "opt_count": {"type": ["integer", "null"]},
                    "tags": {"type": "array", "items": {"type": "string"}},
                    "opt_tags": {
                        "type": "array",
                        "items": {"type": "string", "format": "uuid"},
                    },
                    "child": {"$ref": "#/components/schemas/Child"},
                    "meta": {
                        "type": "object",
                        "additionalProperties": {"type": "string"},
                    },
                    "raw": {"type": "object"},
                    "kind": {"type": "string", "enum": ["a", "b"]},
                    "state": {"type": "string", "enum": ["on", "off"]},
                },
            },
        }
    },
    "paths": {},
}


def openapi_sources():
    problems = []
    go = ccd.merge_go_sources([GO_LEGAL], problems)
    ts = ccd.merge_ts_sources([TS_LEGAL], problems)
    assert problems == []
    return go, ts


def run_openapi(document):
    go, ts = openapi_sources()
    return ccd.check_openapi_pairs(
        PAIRS,
        {"Widget": "Widget", "Child": "Child"},
        go,
        ts,
        document,
        NESTED,
    )


class OpenAPIContractTests(unittest.TestCase):
    def test_legal_openapi_contract_passes(self):
        import copy
        self.assertEqual(run_openapi(copy.deepcopy(OPENAPI_LEGAL)), [])

    def test_missing_component_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        del document["components"]["schemas"]["Widget"]
        self.assertTrue(any(e.startswith("[missing-openapi]") for e in run_openapi(document)))

    def test_field_set_drift_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        del document["components"]["schemas"]["Widget"]["properties"]["name"]
        self.assertTrue(any(e.startswith("[openapi-fields]") for e in run_openapi(document)))

    def test_required_drift_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["required"].remove("name")
        self.assertTrue(any(e.startswith("[openapi-required]") for e in run_openapi(document)))

    def test_required_pointer_must_allow_null(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["properties"]["when_or_null"] = {
            "type": "string", "format": "date-time"
        }
        self.assertTrue(any(e.startswith("[openapi-null]") for e in run_openapi(document)))

    def test_scalar_type_and_format_drift_fail(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["properties"]["id"] = {
            "type": "integer"
        }
        errors = run_openapi(document)
        self.assertTrue(any(e.startswith("[openapi-type]") for e in errors), errors)
        self.assertTrue(any(e.startswith("[openapi-format]") for e in errors), errors)

    def test_array_item_drift_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["properties"]["tags"]["items"] = {
            "type": "integer"
        }
        self.assertTrue(any(e.startswith("[openapi-type]") for e in run_openapi(document)))

    def test_typed_map_requires_additional_properties_schema(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["properties"]["meta"].pop(
            "additionalProperties"
        )
        self.assertTrue(any(e.startswith("[openapi-map]") for e in run_openapi(document)))

    def test_enum_drift_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["properties"]["state"]["enum"] = [
            "on"
        ]
        self.assertTrue(any(e.startswith("[openapi-enum]") for e in run_openapi(document)))

    def test_nested_reference_drift_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["components"]["schemas"]["Widget"]["properties"]["child"] = {
            "type": "object"
        }
        self.assertTrue(any(e.startswith("[openapi-ref]") for e in run_openapi(document)))

    def test_missing_local_reference_fails(self):
        import copy
        document = copy.deepcopy(OPENAPI_LEGAL)
        document["paths"] = {
            "/widgets": {
                "get": {
                    "responses": {
                        "200": {
                            "content": {
                                "application/json": {
                                    "schema": {"$ref": "#/components/schemas/Missing"}
                                }
                            }
                        }
                    }
                }
            }
        }
        errors = ccd._check_local_openapi_refs(document)
        self.assertTrue(any(e.startswith("[openapi-ref-missing]") for e in errors))

    def test_duplicate_yaml_key_is_rejected(self):
        document, errors = ccd.parse_openapi_text(
            "openapi: 3.1.0\ncomponents:\n  schemas:\n    A: {}\n    A: {}\n"
        )
        self.assertIsNone(document)
        self.assertTrue(any(e.startswith("[openapi-yaml]") for e in errors), errors)

    def test_wrong_openapi_version_is_rejected(self):
        document, errors = ccd.parse_openapi_text("openapi: 3.0.3\ncomponents: {}\n")
        self.assertIsNotNone(document)
        self.assertTrue(any(e.startswith("[openapi-version]") for e in errors), errors)


class OpenAPIFailClosedTests(unittest.TestCase):
    def test_extra_scalar_type_cannot_hide_behind_expected_type(self):
        import copy
        d = copy.deepcopy(OPENAPI_LEGAL)
        d['components']['schemas']['Widget']['properties']['count']['type'] = ['integer', 'string']
        self.assertTrue(run_openapi(d))

    def test_extra_array_type_is_rejected(self):
        import copy
        d = copy.deepcopy(OPENAPI_LEGAL)
        d['components']['schemas']['Widget']['properties']['tags']['type'] = ['array', 'integer']
        self.assertTrue(run_openapi(d))

    def test_unrelated_union_branch_cannot_hide_behind_nested_ref(self):
        import copy
        d = copy.deepcopy(OPENAPI_LEGAL)
        d['components']['schemas']['Widget']['properties']['child'] = {
            'anyOf': [{'$ref': '#/components/schemas/Child'}, {'type': 'integer'}]
        }
        self.assertTrue(run_openapi(d))

    def test_legacy_nullable_is_not_json_schema_null(self):
        import copy
        d = copy.deepcopy(OPENAPI_LEGAL)
        d['components']['schemas']['Widget']['properties']['when_or_null'] = {
            'type': 'string', 'format': 'date-time', 'nullable': True
        }
        self.assertTrue(run_openapi(d))

    def test_duplicate_required_is_rejected(self):
        import copy
        d = copy.deepcopy(OPENAPI_LEGAL)
        d['components']['schemas']['Widget']['required'].append('id')
        self.assertTrue(run_openapi(d))

    def test_missing_ts_property_is_reported_not_a_key_error(self):
        import copy
        go, ts = openapi_sources()
        del ts.interfaces['Widget']['count']
        errors = ccd.check_openapi_pairs(PAIRS, NESTED, go, ts, copy.deepcopy(OPENAPI_LEGAL), NESTED)
        self.assertTrue(errors)

    def test_malformed_components_is_reported(self):
        self.assertTrue(run_openapi({'components': []}))

    def test_missing_yaml_dependency_is_reported(self):
        from unittest.mock import patch
        with patch.object(ccd, 'yaml', None):
            doc, errors = ccd.parse_openapi_text('openapi: 3.1.0\n')
        self.assertIsNone(doc)
        self.assertTrue(any(e.startswith('[openapi-dependency]') for e in errors))

    def test_missing_response_ref_is_rejected(self):
        d = {'components': {'schemas': {}}, 'paths': {'/x': {
            'get': {'responses': {'200': {'$ref': '#/components/responses/Missing'}}}
        }}}
        self.assertTrue(ccd._check_local_openapi_refs(d))

    def test_remote_ref_is_not_silently_unchecked(self):
        self.assertTrue(ccd._check_local_openapi_refs({
            'components': {'schemas': {}}, 'x': {'$ref': 'https://example.invalid/schema'}
        }))


if __name__ == '__main__':
    unittest.main()
