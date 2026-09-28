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


if __name__ == '__main__':
    unittest.main()
