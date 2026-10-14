"""Mutation regressions against the checked-in API, not a toy schema alone."""
import copy
import importlib.util
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('openapi_gate', ROOT / 'scripts/check_contract_drift.py')
gate = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = gate
spec.loader.exec_module(gate)


class OpenAPIRepositoryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.document, errors = gate.parse_openapi_text((ROOT / 'internal/api/openapi.yaml').read_text())
        if errors:
            raise AssertionError(errors)
        cls.inventory = json.loads((ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
        cls.rows = [r for r in cls.inventory if r['path'].startswith('/api/v1/company/')]
        problems = []
        texts = [p.read_text() for p in sorted((ROOT / 'internal/company').glob('*.go')) if not p.name.endswith('_test.go')]
        texts.append((ROOT / 'internal/models/mailbox_grants.go').read_text())
        cls.go = gate.merge_go_sources(texts, problems)
        shared_go = gate.merge_go_sources([(ROOT / 'internal/models/models.go').read_text()], problems)
        cls.go.string_types |= shared_go.string_types
        cls.ts = gate.merge_ts_sources([(ROOT / p).read_text() for p in (
            'web/lib/company.ts', 'web/lib/receipt-types.ts',
            'web/features/mail/api.ts', 'web/features/company/api.ts')], problems)
        if problems:
            raise AssertionError(problems)

    def check_dtos(self, document):
        return gate.check_openapi_pairs(gate.company_pairs, gate.OPENAPI_COMPANY_COMPONENTS,
                                       self.go, self.ts, document, gate.OPENAPI_COMPONENTS)

    def test_real_repository_contract(self):
        self.assertEqual(self.check_dtos(self.document), [])
        self.assertEqual(gate.check_company_operations(self.document, self.inventory), [])
        self.assertEqual(gate.check_input_schemas(self.document), [])
        self.assertEqual(gate._check_local_openapi_refs(self.document), [])

    def test_every_mapped_dto_rejects_missing_fields(self):
        for component in gate.OPENAPI_COMPANY_COMPONENTS.values():
            with self.subTest(component=component):
                d = copy.deepcopy(self.document)
                props = gate.resolve_schema_alias(d, d['components']['schemas'][component])['properties']
                del props[next(iter(props))]
                self.assertTrue(self.check_dtos(d))

    def test_every_company_route_is_required(self):
        for row in self.rows:
            with self.subTest(method=row['method'], path=row['path']):
                d = copy.deepcopy(self.document)
                del d['paths'][row['path']][row['method'].lower()]
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_every_typed_operation_rejects_wrong_dto(self):
        for row in self.rows:
            binding = gate.COMPANY_RESPONSES.get(row['handler'])
            if binding is None:
                continue
            with self.subTest(method=row['method'], path=row['path']):
                d = copy.deepcopy(self.document)
                status, _, _ = binding
                s = d['paths'][row['path']][row['method'].lower()]['responses'][status]['content']['application/json']['schema']
                s['properties']['data'] = {'$ref': '#/components/schemas/Error'}
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_paginated_responses_require_meta(self):
        for row in self.rows:
            binding = gate.COMPANY_RESPONSES.get(row['handler'])
            if binding is None or binding[1] != 'page':
                continue
            with self.subTest(path=row['path']):
                d = copy.deepcopy(self.document)
                s = d['paths'][row['path']][row['method'].lower()]['responses'][binding[0]]['content']['application/json']['schema']
                s['properties'].pop('meta')
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_wrong_status_cannot_pass(self):
        d = copy.deepcopy(self.document)
        responses = d['paths']['/api/v1/company/domains']['post']['responses']
        responses['200'] = responses.pop('201')
        self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_unconfigured_settings_must_allow_null(self):
        d = copy.deepcopy(self.document)
        s = d['paths']['/api/v1/company/settings']['get']['responses']['200']['content']['application/json']['schema']
        s['properties']['data'] = {'$ref': '#/components/schemas/CompanySettings'}
        self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_input_cannot_reuse_response_required_fields(self):
        for name in gate.INPUT_REQUIRED:
            with self.subTest(component=name):
                d = copy.deepcopy(self.document)
                d['components']['schemas'][name].setdefault('required', []).append('updated_at')
                self.assertTrue(gate.check_input_schemas(d))

    def test_input_route_cannot_reuse_response_dto(self):
        for row in self.rows:
            if row['handler'] not in gate.COMPANY_REQUESTS:
                continue
            with self.subTest(method=row['method'], path=row['path']):
                d = copy.deepcopy(self.document)
                op = d['paths'][row['path']][row['method'].lower()]
                op['requestBody']['content']['application/json']['schema'] = {'$ref': '#/components/schemas/CompanyDraft'}
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_reconcile_cannot_demand_protocol_response_fields(self):
        d = copy.deepcopy(self.document)
        items = d['paths']['/api/v1/company/outbound/{id}/reconcile']['post']['requestBody']['content']['application/json']['schema']['properties']['results']
        items['items'] = {'$ref': '#/components/schemas/CompanyRecipient'}
        self.assertTrue(gate.check_input_schemas(d))

    def test_unknown_inventory_and_malformed_envelope_fail(self):
        for inventory in ([], None, [{}], [{'method': 'GET', 'path': '/x', 'handler': 'h'}]):
            self.assertTrue(gate.check_company_operations(self.document, inventory))
        d = copy.deepcopy(self.document)
        s = d['paths']['/api/v1/company/overview']['get']['responses']['200']['content']['application/json']['schema']
        s['required'] = True
        self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_extra_undocumented_operation_is_not_ignored(self):
        d = copy.deepcopy(self.document)
        d['paths']['/api/v1/company/phantom'] = {'get': {'responses': {}}}
        self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_real_enum_order_is_not_semantic_drift(self):
        d = copy.deepcopy(self.document)
        gate.resolve_schema_alias(d, d['components']['schemas']['Submission'])['properties']['status']['enum'].reverse()
        self.assertEqual(self.check_dtos(d), [])

    def test_storage_secrets_cannot_be_added_to_company_dtos(self):
        for field in ('object_key', 'raw_mime', 'delivery_token'):
            d = copy.deepcopy(self.document)
            d['components']['schemas']['SubmissionAttachment']['properties'][field] = {'type': 'string'}
            self.assertTrue(self.check_dtos(d))


class OpenAPIParserSafetyTests(unittest.TestCase):
    def test_aliases_and_python_constructors_are_rejected_without_execution(self):
        for text in ('openapi: 3.1.0\na: &x [*x]\n',
                     'openapi: 3.1.0\na: !!python/object/apply:os.system ["false"]\n',
                     'openapi: 3.1.0\n? [a, b]\n: value\n'):
            document, errors = gate.parse_openapi_text(text)
            self.assertIsNone(document)
            self.assertTrue(errors)

    def test_size_and_depth_are_bounded(self):
        for text in ('x: ' + 'a' * (2 * 1024 * 1024),
                     'openapi: 3.1.0\nx: ' + '[' * 120 + '0' + ']' * 120):
            document, errors = gate.parse_openapi_text(text)
            self.assertIsNone(document)
            self.assertTrue(errors)

    def test_refs_use_local_json_pointer_escapes(self):
        document = {'components': {'schemas': {'a/b~c': {'type': 'string'}}},
                    'path': {'$ref': '#/components/schemas/a~1b~0c'}}
        self.assertEqual(gate._check_local_openapi_refs(document), [])
        document['path']['$ref'] = '#/components/schemas/a~2'
        self.assertTrue(gate._check_local_openapi_refs(document))


if __name__ == '__main__':
    unittest.main()
