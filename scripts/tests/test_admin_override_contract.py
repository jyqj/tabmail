"""Mutation controls against the actual raw override Go/TS/OpenAPI contract."""
import copy
import importlib.util
import json
from pathlib import Path
import sys
import unittest

spec = importlib.util.spec_from_file_location('admin_ccd', Path(__file__).resolve().parents[1] / 'check_contract_drift.py')
ccd = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = ccd
spec.loader.exec_module(ccd)


class AdminOverrideContractTests(unittest.TestCase):
    def setUp(self):
        self.go = (ccd.ROOT / 'internal/app/admin/tenant_override.go').read_text()
        self.ts = ccd.TS_TYPES.read_text()
        self.document, errors = ccd.parse_openapi_text(ccd.OPENAPI_SPEC.read_text())
        self.assertEqual([], errors)
        self.routes = json.loads((ccd.ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
        self.assertEqual([], self.check(), 'the unmodified production contract must pass first')

    def check(self):
        return ccd.check_admin_tenant_override_contract(self.document, self.routes, self.go, self.ts)

    def schema(self):
        return self.document['components']['schemas']['TenantOverrideSnapshot']

    def operation(self):
        return self.document['paths']['/api/v1/admin/tenants/{id}']['get']

    def route(self):
        return next(r for r in self.routes if r['method'] == 'GET' and r['path'] == '/api/v1/admin/tenants/{id}')

    def test_actual_contract(self):
        self.assertEqual([], self.check())

    def test_missing_go_dto(self):
        self.go = self.go.replace('type TenantOverrideSnapshot struct', 'type RemovedSnapshot struct')
        self.assertTrue(self.check())

    def test_missing_ts_dto(self):
        self.ts = self.ts.replace('interface TenantOverrideSnapshot', 'interface RemovedSnapshot')
        self.assertTrue(self.check())

    def test_go_omitted_nullable_field(self):
        self.go = self.go.replace('json:"max_domains"', 'json:"max_domains,omitempty"')
        self.assertTrue(self.check())

    def test_ts_optional_field(self):
        start = self.ts.index('interface TenantOverrideSnapshot')
        self.ts = self.ts[:start] + self.ts[start:].replace('max_domains:', 'max_domains?:', 1)
        self.assertTrue(self.check())

    def test_ts_nonnullable_field(self):
        start = self.ts.index('interface TenantOverrideSnapshot')
        self.ts = self.ts[:start] + self.ts[start:].replace('max_domains: number | null', 'max_domains: number', 1)
        self.assertTrue(self.check())

    def test_missing_schema_field(self):
        del self.schema()['properties']['max_domains']
        self.assertTrue(self.check())

    def test_optional_schema_field(self):
        self.schema()['required'].remove('max_domains')
        self.assertTrue(self.check())

    def test_nonnullable_schema_field(self):
        self.schema()['properties']['max_domains']['type'] = 'integer'
        self.assertTrue(self.check())

    def test_open_schema(self):
        self.schema()['additionalProperties'] = True
        self.assertTrue(self.check())

    def test_wrong_numeric_bound(self):
        self.schema()['properties']['max_domains']['maximum'] = 2147483648
        self.assertTrue(self.check())

    def test_missing_handler(self):
        self.routes.remove(self.route())
        self.assertTrue(self.check())

    def test_duplicate_handler(self):
        self.routes.append(copy.deepcopy(self.route()))
        self.assertTrue(self.check())

    def test_wrong_handler(self):
        self.route()['handler'] = 'adm.GetEffectiveConfig'
        self.assertTrue(self.check())

    def test_missing_superadmin_middleware(self):
        self.route()['middleware'].remove('middleware.RequireSuperAdmin')
        self.assertTrue(self.check())

    def test_missing_auth_middleware(self):
        self.route()['middleware'] = [m for m in self.route()['middleware'] if not m.startswith('middleware.Auth(')]
        self.assertTrue(self.check())

    def test_missing_security(self):
        self.operation()['security'] = []
        self.assertTrue(self.check())

    def test_wrong_path_parameter(self):
        self.operation()['parameters'][0]['schema']['format'] = 'email'
        self.assertTrue(self.check())

    def test_extra_success_status(self):
        self.operation()['responses']['204'] = {'description': 'no data'}
        self.assertTrue(self.check())

    def test_missing_envelope_requirement(self):
        self.operation()['responses']['200']['content']['application/json']['schema']['required'] = []
        self.assertTrue(self.check())

    def test_effective_response_is_not_raw_snapshot(self):
        self.operation()['responses']['200']['content']['application/json']['schema']['properties']['data'] = {'$ref': '#/components/schemas/EffectiveConfig'}
        self.assertTrue(self.check())


if __name__ == '__main__':
    unittest.main()
