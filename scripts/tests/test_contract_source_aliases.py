"""Source/schema alias regressions; these are not HTTP/runtime witnesses."""
import copy
import importlib.util
from pathlib import Path
import sys
import unittest


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('contract_source_alias_gate', ROOT / 'scripts/check_contract_drift.py')
gate = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = gate
spec.loader.exec_module(gate)

GO = '''
package company
type Submission = Bridge
type Bridge = OutboundReceipt
type OutboundReceipt struct {
    ID string `json:"id"`
}
type SubmissionContent struct {
    ID string `json:"id"`
    BCC []string `json:"bcc"`
    Completeness string `json:"recipient_completeness"`
}
'''
TS = '''
export type Submission = Bridge;
export type Bridge = OrdinaryReceipt;
export interface OrdinaryReceipt { id: string; }
export type SubmissionContent = LiveContent;
export interface LiveContent {
    id: string;
    bcc: string[] | null;
    recipient_completeness: "complete" | "legacy_unknown";
}
'''
PAIRS = {'Submission': 'Submission', 'SubmissionContent': 'SubmissionContent'}
SCHEMAS = {
    'Submission': {'$ref': '#/components/schemas/ReceiptAlias'},
    'ReceiptAlias': {'$ref': '#/components/schemas/OutboundReceipt'},
    'OutboundReceipt': {
        'type': 'object', 'additionalProperties': False,
        'required': ['id'], 'properties': {'id': {'type': 'string'}},
    },
    'SubmissionContent': {
        'type': 'object', 'additionalProperties': False,
        'required': ['id', 'bcc', 'recipient_completeness'],
        'properties': {
            'id': {'type': 'string'},
            'bcc': {'type': ['array', 'null'], 'items': {'type': 'string'}},
            'recipient_completeness': {'type': 'string', 'enum': ['complete', 'legacy_unknown']},
        },
    },
}


def sources(go=GO, ts=TS):
    errors = []
    return gate.merge_go_sources([go], errors), gate.merge_ts_sources([ts], errors), errors


def check_schemas(document=None, go=GO, ts=TS):
    go_source, ts_source, errors = sources(go, ts)
    if document is None:
        document = {'components': {'schemas': copy.deepcopy(SCHEMAS)}}
    errors += gate.check_openapi_pairs(PAIRS, PAIRS, go_source, ts_source, document, PAIRS)
    return errors


class SourceAliasTests(unittest.TestCase):
    def test_go_and_ts_alias_chains_preserve_fields(self):
        self.assertEqual(gate.check_pairs(PAIRS, [GO], [TS], PAIRS), [])
        self.assertEqual(check_schemas(), [])

    def test_alias_chain_can_cross_source_files(self):
        go = GO.replace('type Submission = Bridge\n', '')
        ts = TS.replace('export type Submission = Bridge;\n', '')
        self.assertEqual(gate.check_pairs(PAIRS, ['type Submission = Bridge\n', go],
                                         ['export type Submission = Bridge;\n', ts], PAIRS), [])

    def test_missing_go_target_is_not_a_missing_struct_waiver(self):
        errors = gate.check_pairs(PAIRS, [GO.replace('= Bridge', '= Missing')], [TS], PAIRS)
        self.assertTrue(any(e.startswith('[go-alias]') for e in errors), errors)
        self.assertIn('[missing-go] Submission', errors)

    def test_go_alias_cycle_is_rejected(self):
        errors = gate.check_pairs(PAIRS, [GO.replace('Bridge = OutboundReceipt', 'Bridge = Submission')], [TS], PAIRS)
        self.assertTrue(any('cyclic alias' in e for e in errors), errors)

    def test_qualified_go_alias_is_not_guessed_from_short_name(self):
        errors = gate.check_pairs(PAIRS, [GO.replace('Bridge = OutboundReceipt', 'Bridge = other.OutboundReceipt')], [TS], PAIRS)
        self.assertTrue(any('unsupported alias target' in e for e in errors), errors)

    def test_duplicate_go_alias_across_files_is_rejected(self):
        errors = gate.check_pairs(PAIRS, [GO, 'type Submission = OutboundReceipt\n'], [TS], PAIRS)
        self.assertIn('[duplicate-go] Submission', errors)

    def test_go_alias_and_struct_collision_is_rejected(self):
        errors = gate.check_pairs(PAIRS, [GO, 'type Submission struct {\n ID string `json:"id"`\n}\n'], [TS], PAIRS)
        self.assertIn('[duplicate-go] Submission', errors)

    def test_go_alias_string_and_struct_collisions_reject_both_file_orders(self):
        declarations = (
            'type Submission = OutboundReceipt\n',
            'type Submission string\n',
            'type Submission struct {\n ID string `json:"id"`\n}\n',
        )
        target = 'type OutboundReceipt struct {\n ID string `json:"id"`\n}\n'
        for left in range(len(declarations)):
            for right in range(len(declarations)):
                if left == right:
                    continue
                with self.subTest(first=left, second=right):
                    errors = []
                    gate.merge_go_sources([target, declarations[left], declarations[right]], errors)
                    self.assertIn('[duplicate-go] Submission', errors)

    def test_ts_alias_cycle_is_rejected(self):
        errors = gate.check_pairs(PAIRS, [GO], [TS.replace('Bridge = OrdinaryReceipt', 'Bridge = Submission')], PAIRS)
        self.assertTrue(any('cyclic alias' in e for e in errors), errors)

    def test_missing_ts_target_fails_closed(self):
        errors = gate.check_pairs(PAIRS, [GO], [TS.replace('Bridge = OrdinaryReceipt', 'Bridge = Missing')], PAIRS)
        self.assertIn('[missing-ts] Submission', errors)

    def test_duplicate_ts_alias_same_or_different_files_is_rejected(self):
        extra = 'export type Submission = OrdinaryReceipt;\n'
        for texts in ([TS + extra], [TS, extra]):
            with self.subTest(texts=len(texts)):
                self.assertIn('[duplicate-ts] Submission', gate.check_pairs(PAIRS, [GO], texts, PAIRS))

    def test_ts_alias_interface_collision_is_rejected(self):
        extra = 'export interface Submission { id: string; }\n'
        for texts in ([TS + extra], [TS, extra]):
            with self.subTest(texts=len(texts)):
                self.assertIn('[duplicate-ts] Submission', gate.check_pairs(PAIRS, [GO], texts, PAIRS))

    def test_commented_aliases_cannot_override_sources(self):
        self.assertEqual(gate.check_pairs(PAIRS, [GO + '\n/*\ntype Submission = Missing\n*/'],
            [TS + '\n/*\nexport type Submission = Missing;\n*/'], PAIRS), [])

    def test_extra_json_field_on_alias_target_is_rejected(self):
        changed = GO.replace('type OutboundReceipt struct {', 'type OutboundReceipt struct {\n Subject string `json:"subject"`')
        self.assertTrue(any(e.startswith('[drift] Submission') for e in gate.check_pairs(PAIRS, [changed], [TS], PAIRS)))
        self.assertTrue(any(e.startswith('[openapi-fields] Submission') for e in check_schemas(go=changed)))

    def test_nested_ts_reference_alias_is_equivalent(self):
        go = GO + '\ntype Wrapper struct {\n Receipt Submission `json:"receipt"`\n}\n'
        ts = TS + '\nexport interface Wrapper { receipt: OrdinaryReceipt; }\n'
        self.assertEqual(gate.check_pairs({'Wrapper': 'Wrapper'}, [go], [ts], PAIRS), [])

    def test_object_union_field_projection_keeps_optional_counts(self):
        source, errors = gate.parse_ts_source('''
export type Progress = { completeness: "unknown"; counts?: never } | { completeness: "known"; counts: Counts };
export interface Counts { total: number; }
''')
        self.assertEqual(errors, [])
        fields = gate._ts_fields('Progress', source)
        self.assertEqual(set(fields), {'completeness', 'counts'})
        self.assertFalse(fields['completeness'].optional)
        self.assertTrue(fields['counts'].optional)
        self.assertEqual(fields['counts'].ts_type, 'Counts')

    def test_unsupported_union_or_duplicate_alias_fields_fail_closed(self):
        for definition in ('{ id: string } | unknown', '{ id: string; id: number }', 'Missing & Other'):
            with self.subTest(definition=definition):
                ts = f'export type Submission = {definition};\n'
                errors = gate.check_pairs({'Submission': 'Submission'}, [GO], [ts], PAIRS)
                self.assertTrue(any(e.startswith('[ts-alias]') for e in errors), errors)


class SchemaAliasTests(unittest.TestCase):
    def document(self):
        return {'components': {'schemas': copy.deepcopy(SCHEMAS)}}

    def test_schema_alias_cycle_is_rejected(self):
        d = self.document()
        d['components']['schemas']['ReceiptAlias'] = {'$ref': '#/components/schemas/Submission'}
        self.assertTrue(any('cyclic schema alias' in e for e in check_schemas(d)))

    def test_missing_or_non_schema_ref_fails_closed(self):
        for ref in ('#/components/schemas/Missing', '#/paths/x', 'https://example.test/schema'):
            with self.subTest(ref=ref):
                d = self.document()
                d['components']['schemas']['Submission'] = {'$ref': ref}
                self.assertTrue(any(e.startswith('[openapi-alias]') for e in check_schemas(d)))

    def test_alias_validation_siblings_are_not_discarded(self):
        for sibling in ({'properties': {}}, {'allOf': []}, {'additionalProperties': True}):
            with self.subTest(sibling=sibling):
                d = self.document()
                d['components']['schemas']['Submission'].update(sibling)
                self.assertTrue(any('validation siblings' in e for e in check_schemas(d)))

    def test_ambiguous_schema_composition_is_rejected(self):
        d = self.document()
        d['components']['schemas']['Submission'] = {'anyOf': [
            {'$ref': '#/components/schemas/OutboundReceipt'},
            {'$ref': '#/components/schemas/SubmissionContent'},
        ]}
        self.assertTrue(check_schemas(d))

    def test_receipt_and_content_must_remain_independent(self):
        for name, target in (('Submission', 'SubmissionContent'), ('SubmissionContent', 'OutboundReceipt')):
            with self.subTest(name=name):
                d = self.document()
                d['components']['schemas'][name] = {'$ref': '#/components/schemas/' + target}
                self.assertTrue(any(e.startswith('[openapi-fields]') for e in check_schemas(d)))

    def test_ordinary_alias_cannot_gain_content_field(self):
        for field in ('bcc', 'subject', 'object_key', 'smtp_response'):
            with self.subTest(field=field):
                d = self.document()
                d['components']['schemas']['OutboundReceipt']['properties'][field] = {'type': 'string'}
                self.assertTrue(any(e.startswith('[openapi-fields]') for e in check_schemas(d)))

    def test_closed_objects_cannot_reenable_extra_properties(self):
        for name in ('OutboundReceipt', 'SubmissionContent'):
            for value in (True, None, {'type': 'string'}):
                with self.subTest(name=name, value=value):
                    d = self.document()
                    d['components']['schemas'][name]['additionalProperties'] = value
                    self.assertTrue(any(e.startswith('[openapi-closed]') for e in check_schemas(d)))

    def test_missing_content_bcc_or_completeness_is_rejected(self):
        for field in ('bcc', 'recipient_completeness'):
            with self.subTest(field=field):
                d = self.document()
                del d['components']['schemas']['SubmissionContent']['properties'][field]
                self.assertTrue(any(e.startswith('[openapi-fields]') for e in check_schemas(d)))

    def test_legacy_outbound_component_is_bound_to_public_projection(self):
        self.assertEqual(gate.company_pairs['OutboundReceipt'], 'OrdinaryReceipt')
        self.assertEqual(gate.company_pairs['SubmissionContent'], 'SubmissionContent')
        self.assertEqual(gate.COMPANY_RESPONSES['c.Mail.SubmitDraft'], ('201', 'one', 'OutboundReceipt'))
        self.assertEqual(gate.COMPANY_ALTERNATE_RESPONSES['c.Mail.SubmitDraft'], ('200', 'one', 'OutboundReceipt'))


class BooleanLiteralTests(unittest.TestCase):
    def comparisons(self, ts_type, enum, aliases='', pointer=False, optional=False,
                    omitempty=False, nullable=False):
        go_type = '*bool' if pointer else 'bool'
        tag = 'flag,omitempty' if omitempty else 'flag'
        go = f'type Value struct {{\n Flag {go_type} `json:"{tag}"`\n}}\n'
        ts = aliases + f'export interface Value {{ flag{"?" if optional else ""}: {ts_type}; }}\n'
        go_source, ts_source, errors = sources(go, ts)
        source_errors = errors + gate.check_pairs({'Value': 'Value'}, [go], [ts], {})
        value = {'type': ['boolean', 'null'] if nullable else 'boolean'}
        if enum is not None:
            value = ({'anyOf': [{'type': 'boolean', 'enum': enum}, {'type': 'null'}]}
                     if nullable else {'type': 'boolean', 'enum': enum})
        document = {'components': {'schemas': {'Value': {
            'type': 'object', 'required': [] if omitempty else ['flag'], 'properties': {'flag': value},
        }}}}
        schema_errors = gate.check_openapi_pairs({'Value': 'Value'}, {'Value': 'Value'},
                                                go_source, ts_source, document, {})
        return source_errors, schema_errors

    def check(self, ts_type, enum):
        source_errors, schema_errors = self.comparisons(ts_type, enum)
        return source_errors + schema_errors

    def test_boolean_literal_matches_only_same_boolean_enum(self):
        self.assertEqual(self.check('false', [False]), [])
        self.assertEqual(self.check('true', [True]), [])
        self.assertEqual(self.check('false | true', [True, False]), [])

    def test_unconstrained_boolean_is_not_a_false_only_waiver(self):
        self.assertEqual(self.check('boolean', None), [])
        self.assertEqual(self.check('boolean', [False, True]), [])
        self.assertTrue(self.check('boolean', [False]))

    def test_wrong_missing_duplicate_or_mixed_boolean_enum_rejected(self):
        for enum in ([True], None, [False, False], [False, 0], [False, 'false'], [False, {}]):
            with self.subTest(enum=enum):
                self.assertTrue(self.check('false', enum))

    def test_unknown_and_non_boolean_ts_types_are_rejected(self):
        for ts_type in ('unknown', 'Missing', 'false | "false"', 'false | number', '0'):
            with self.subTest(ts_type=ts_type):
                self.assertTrue(self.check(ts_type, [False]))

    def test_nullable_and_null_only_boolean_aliases_reject_in_both_gates(self):
        for aliases in (
            'export type ContentFlag = false | null;\n',
            'export type ContentFlag = null;\n',
            'export type ContentFlag = undefined;\n',
            'export type ContentFlag = null | undefined;\n',
            'export type ContentFlag = InnerFlag;\nexport type InnerFlag = false | null;\n',
        ):
            with self.subTest(aliases=aliases):
                for errors in self.comparisons('ContentFlag', [False], aliases=aliases):
                    self.assertTrue(errors)

    def test_null_only_alias_cannot_stand_for_a_nullable_go_pointer(self):
        for errors in self.comparisons('ContentFlag', None,
                aliases='export type ContentFlag = null;\n', pointer=True, nullable=True):
            self.assertTrue(errors)

    def test_nullable_boolean_alias_matches_nullable_pointer_and_schema(self):
        source_errors, schema_errors = self.comparisons('ContentFlag', [False],
            aliases='export type ContentFlag = false | null;\n', pointer=True, nullable=True)
        self.assertEqual(source_errors, [])
        self.assertEqual(schema_errors, [])

    def test_required_field_cannot_hide_undefined_or_optional_presence(self):
        for optional, aliases in (
            (False, 'export type ContentFlag = false | undefined;\n'),
            (True, 'export type ContentFlag = false;\n'),
            (True, 'export type ContentFlag = InnerFlag;\nexport type InnerFlag = false | undefined;\n'),
        ):
            with self.subTest(optional=optional, aliases=aliases):
                for errors in self.comparisons('ContentFlag', [False], aliases=aliases, optional=optional):
                    self.assertTrue(errors)

    def test_optional_undefined_alias_keeps_omitempty_but_does_not_invent_null(self):
        aliases = 'export type ContentFlag = false | undefined;\n'
        self.assertEqual(self.comparisons('ContentFlag', [False], aliases=aliases,
                         optional=True, omitempty=True), ([], []))
        for errors in self.comparisons('ContentFlag', [False], aliases=aliases, omitempty=True):
            self.assertTrue(errors)


class CollectionAliasScopeTests(unittest.TestCase):
    def comparisons(self, go_type, ts_type, schema, aliases, optional=False):
        tag = 'value,omitempty' if optional else 'value'
        go = f'type Value struct {{\n Value {go_type} `json:"{tag}"`\n}}\n'
        ts = aliases + f'export interface Value {{ value{"?" if optional else ""}: {ts_type}; }}\n'
        go_source, ts_source, errors = sources(go, ts)
        self.assertEqual(errors, [])
        document = {'components': {'schemas': {'Value': {
            'type': 'object', 'required': [] if optional else ['value'],
            'properties': {'value': schema},
        }}}}
        return (gate.check_pairs({'Value': 'Value'}, [go], [ts], {}),
                gate.check_openapi_pairs({'Value': 'Value'}, {'Value': 'Value'},
                                        go_source, ts_source, document, {}))

    def assert_both_reject(self, *args, **kwargs):
        for phase, errors in zip(('Go/TS', 'OpenAPI'), self.comparisons(*args, **kwargs)):
            self.assertTrue(errors, phase)

    def test_current_bcc_nullable_container_cannot_hide_nullable_address_items(self):
        ts = 'export type Address = string | null;\n' + TS.replace(
            'bcc: string[] | null;', 'bcc: Address[] | null;')
        self.assertTrue(gate.check_pairs(PAIRS, [GO], [ts], PAIRS))
        self.assertTrue(check_schemas(ts=ts))

    def test_string_and_boolean_array_items_reject_null_and_undefined_aliases(self):
        for go_type, base, items in (('string', 'string', {'type': 'string'}),
                                     ('bool', 'false', {'type': 'boolean', 'enum': [False]})):
            for nullish in ('null', 'undefined'):
                for container in ('Item[] | null', 'Array<Item> | null'):
                    with self.subTest(go_type=go_type, nullish=nullish, container=container):
                        self.assert_both_reject('[]' + go_type, container,
                            {'type': ['array', 'null'], 'items': items},
                            f'export type Item = {base} | {nullish};\n')

    def test_nested_array_and_alias_chains_do_not_flatten_element_nullability(self):
        schema = {'type': ['array', 'null'], 'items': {'type': 'array', 'items': {'type': 'string'}}}
        for nullish in ('null', 'undefined'):
            aliases = ('export type Rows = Item[];\nexport type Item = Address;\n'
                       f'export type Address = string | {nullish};\n')
            for ts_type in ('Rows[] | null', 'Item[][] | null', 'Array<Array<Item>> | null'):
                with self.subTest(nullish=nullish, ts_type=ts_type):
                    self.assert_both_reject('[][]string', ts_type, schema, aliases)

    def test_every_array_union_branch_is_checked_not_only_the_first(self):
        self.assert_both_reject('[]string', 'string[] | Address[] | null',
            {'type': ['array', 'null'], 'items': {'type': 'string'}},
            'export type Address = string | null;\n')

    def test_record_value_and_nested_array_value_have_independent_null_domains(self):
        for nullish in ('null', 'undefined'):
            with self.subTest(nullish=nullish):
                self.assert_both_reject('map[string]string', 'Record<string, Item> | null',
                    {'type': ['object', 'null'], 'additionalProperties': {'type': 'string'}},
                    f'export type Item = string | {nullish};\n')
                self.assert_both_reject('map[string][]bool', 'Record<string, Item[]> | null',
                    {'type': ['object', 'null'], 'additionalProperties': {
                        'type': 'array', 'items': {'type': 'boolean', 'enum': [False]}}},
                    f'export type Item = false | {nullish};\n')

    def test_optional_container_undefined_permission_does_not_reach_items(self):
        schema = {'type': 'array', 'items': {'type': 'boolean', 'enum': [False]}}
        self.assert_both_reject('[]bool', 'Item[] | undefined', schema,
            'export type Item = false | undefined;\n', optional=True)
        self.assertEqual(self.comparisons('[]bool', 'Item[] | undefined', schema,
            'export type Item = false;\n', optional=True), ([], []))

    def test_pointer_elements_and_nested_arrays_keep_their_own_valid_nullability(self):
        self.assertEqual(self.comparisons('[]*string', 'Item[] | null',
            {'type': ['array', 'null'], 'items': {'type': ['string', 'null']}},
            'export type Item = string | null;\n'), ([], []))
        schema = {'type': ['array', 'null'], 'items': {'type': 'array', 'items': {'type': 'string'}}}
        self.assertEqual(self.comparisons('[][]string', 'Item[][] | null', schema,
            'export type Item = string;\n'), ([], []))

    def test_widening_both_ts_and_schema_items_still_cannot_widen_go_scalar(self):
        self.assert_both_reject('[]string', 'Item[] | null',
            {'type': ['array', 'null'], 'items': {'type': ['string', 'null']}},
            'export type Item = string | null;\n')


if __name__ == '__main__':
    unittest.main()
