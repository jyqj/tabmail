"""Focused checked-in OpenAPI schema witnesses; not HTTP/PG acceptance.

The tiny evaluator implements only keywords used by editor input schemas;
structural assertions and mutation witnesses protect this scope explicitly.
"""
import copy
import importlib.util
from pathlib import Path
import re
import sys
import unittest
import uuid

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('permission_openapi_gate', ROOT / 'scripts/check_contract_drift.py')
gate = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = gate
spec.loader.exec_module(gate)
DOC, ERRORS = gate.parse_openapi_text((ROOT / 'internal/api/openapi.yaml').read_text())
SCHEMAS = DOC['components']['schemas'] if DOC else {}
U = '11111111-1111-4111-8111-111111111111'
V = '22222222-2222-4222-8222-222222222222'
REV = dict(user_id=U, tenant_id=V, user_revision='1', profile_id=None, profile_revision=None)


def accepts(schema, value):
    if '$ref' in schema:
        return accepts(SCHEMAS[schema['$ref'].split('/')[-1]], value)
    if 'anyOf' in schema and not any(accepts(s, value) for s in schema['anyOf']):
        return False
    if 'oneOf' in schema and sum(accepts(s, value) for s in schema['oneOf']) != 1:
        return False
    if 'not' in schema and accepts(schema['not'], value):
        return False
    if 'const' in schema and value != schema['const']:
        return False
    if 'enum' in schema and value not in schema['enum']:
        return False
    types = schema.get('type')
    types = [types] if isinstance(types, str) else types
    kind = 'null' if value is None else 'boolean' if isinstance(value, bool) else 'integer' if isinstance(value, int) else 'string' if isinstance(value, str) else 'object' if isinstance(value, dict) else 'array' if isinstance(value, list) else 'number'
    if types and kind not in types:
        return False
    if isinstance(value, dict):
        if any(k not in value for k in schema.get('required', [])):
            return False
        props = schema.get('properties', {})
        if schema.get('additionalProperties') is False and set(value) - set(props):
            return False
        if any(not accepts(props[k], v) for k, v in value.items() if k in props):
            return False
    if isinstance(value, list):
        if len(value) < schema.get('minItems', 0) or len(value) > schema.get('maxItems', float('inf')):
            return False
        if schema.get('uniqueItems') and any(v in value[:i] for i, v in enumerate(value)):
            return False
        if 'items' in schema and not all(accepts(schema['items'], v) for v in value):
            return False
    if isinstance(value, int) and not isinstance(value, bool):
        if value < schema.get('minimum', -float('inf')) or value > schema.get('maximum', float('inf')):
            return False
    if isinstance(value, str):
        if 'pattern' in schema and re.fullmatch(schema['pattern'], value) is None:
            return False
        if schema.get('format') == 'uuid':
            try:
                uuid.UUID(value)
            except ValueError:
                return False
    return True


class PermissionEditorContractTests(unittest.TestCase):
    def test_document_and_refs(self):
        self.assertEqual(ERRORS, [])
        self.assertEqual(gate._check_local_openapi_refs(DOC), [])
        for name, schema in SCHEMAS.items():
            if name.startswith('PermissionEditor'):
                self.assertNotIn('nullable', repr(schema))

    def command(self, patch, revision=None):
        return dict(expected_revision=REV if revision is None else revision, patch=patch)

    def valid(self, value):
        return accepts(SCHEMAS['PermissionEditorCommand'], value)

    def test_field_intent_and_empty_patch(self):
        for patch in ({}, {'can_send': False}, {'daily_send_quota': 0}, {'can_send': None}, {'domain_access': None}):
            self.assertTrue(self.valid(self.command(patch)), patch)
        for patch in ({'can_send': 0}, {'daily_send_quota': False}, {'daily_send_quota': -1}, {'daily_send_quota': 1.5}, {'daily_send_quota': '1'}, {'unexpected': True}):
            self.assertFalse(self.valid(self.command(patch)), patch)
        self.assertFalse(self.valid({'patch': {}}))
        self.assertFalse(self.valid({'expected_revision': REV}))
        self.assertFalse(self.valid({**self.command({}), 'profile_id': U}))

    def test_domain_modes_are_not_empty_allowlist_inference(self):
        for mode in ('inherit', 'all', 'none'):
            for zones in ('omitted', None, []):
                value = dict(mode=mode)
                if zones != 'omitted':
                    value['zone_ids'] = zones
                self.assertTrue(self.valid(self.command({'domain_access': value})))
            self.assertFalse(self.valid(self.command({'domain_access': dict(mode=mode, zone_ids=[U])})))
        self.assertTrue(self.valid(self.command({'domain_access': dict(mode='list', zone_ids=[U, V])})))
        for value in (dict(mode='list'), dict(mode='list', zone_ids=[]), dict(mode='list', zone_ids=None), dict(mode='list', zone_ids=[U, U]), dict(mode='list', zone_ids=['bad']), dict(mode='list', zone_ids=['00000000-0000-0000-0000-000000000000']), dict(mode='none', unknown=True), dict(mode='invalid')):
            self.assertFalse(self.valid(self.command({'domain_access': value})), value)

    def test_compound_revision_int64_and_pairing(self):
        for value in ('1', '9007199254740993', '9223372036854775807'):
            self.assertTrue(self.valid(self.command({}, {**REV, 'user_revision': value})))
        for value in ('0', '-1', '01', '1.0', '9223372036854775808', '9999999999999999999', 1):
            self.assertFalse(self.valid(self.command({}, {**REV, 'user_revision': value})))
        self.assertTrue(self.valid(self.command({}, {**REV, 'profile_id': U, 'profile_revision': '1'})))
        for revision in ({**REV, 'profile_id': U}, {**REV, 'profile_revision': '1'}, {**REV, 'extra': '1'}, {**REV, 'tenant_id': None}):
            self.assertFalse(self.valid(self.command({}, revision)))
        for key in REV:
            revision = dict(REV)
            revision.pop(key)
            self.assertFalse(self.valid(self.command({}, revision)))

    def test_snapshot_raw_presence_and_field_sources(self):
        props = SCHEMAS['PermissionEditorSnapshot']['properties']
        self.assertEqual(set(SCHEMAS['PermissionEditorSnapshot']['required']), set(props))
        for key in ('profile', 'overrides'):
            self.assertTrue(gate._schema_nullable(props[key]))
        raw = SCHEMAS['PermissionEditorRawOverrides']
        self.assertEqual(set(raw['required']), set(raw['properties']))
        for key in ('can_send', 'daily_send_quota'):
            self.assertTrue(gate._schema_nullable(raw['properties'][key]))
        sources = SCHEMAS['PermissionEditorFieldSources']
        self.assertEqual(len(sources['required']), 9)
        self.assertIn('domain_access', sources['required'])
        self.assertEqual(SCHEMAS['PermissionEditorCapabilities']['properties']['assign_profile']['const'], False)

    def test_effective_canonical_domain_mode_is_optional_not_inherited(self):
        schema = SCHEMAS['EffectivePermission']
        mode = schema['properties']['domain_access_mode']
        self.assertEqual(mode['enum'], ['all', 'list', 'none'])
        self.assertNotIn('domain_access_mode', schema.get('required', []))
        for value in ('all', 'list', 'none'):
            self.assertTrue(accepts(mode, value))
        for value in ('inherit', '', None):
            self.assertFalse(accepts(mode, value))

    def test_routes_envelopes_and_legacy_reads(self):
        route = DOC['paths']['/api/v1/admin/users/{id}/permission-editor']
        self.assertEqual(set(route), {'get', 'patch'})
        for method in route:
            op = route[method]
            self.assertEqual(op['security'], [{'BearerAuth': []}])
            self.assertEqual(op['responses']['200']['content']['application/json']['schema']['$ref'], '#/components/schemas/PermissionEditorResponse')
            for status in ('400', '401', '403', '404', '409', '500'):
                self.assertEqual(op['responses'][status]['content']['application/json']['schema']['$ref'], '#/components/schemas/PermissionEditorError')
        self.assertEqual(route['patch']['requestBody']['content']['application/json']['schema']['$ref'], '#/components/schemas/PermissionEditorCommand')
        for path in ('/api/v1/admin/users/{id}/permissions', '/api/v1/auth/me/permissions'):
            schema = DOC['paths'][path]['get']['responses']['200']['content']['application/json']['schema']
            self.assertEqual(schema['properties']['data']['$ref'], '#/components/schemas/EffectivePermission')

    def test_go_dto_json_fields_match_checked_in_schema(self):
        source = (ROOT / 'internal/company/permission_editor.go').read_text()
        for typename, component in (
            ('PermissionRevision', 'PermissionEditorRevision'),
            ('PermissionPatch', 'PermissionEditorPatch'),
            ('PermissionEditorCommand', 'PermissionEditorCommand'),
            ('RawPermissionOverrides', 'PermissionEditorRawOverrides'),
            ('PermissionEditorCapabilities', 'PermissionEditorCapabilities'),
            ('PermissionEditorSnapshot', 'PermissionEditorSnapshot'),
            ('PermissionAssignmentCommand', 'PermissionEditorAssignmentCommand'),
            ('PermissionProfileDeletionPreview', 'PermissionProfileDeletionPreview'),
            ('PermissionProfileDeletionChange', 'PermissionProfileDeletionChange'),
        ):
            match = re.search(r'type ' + typename + r' struct \{(.*?)\n?\}', source, re.S)
            self.assertIsNotNone(match, typename)
            tags = set(re.findall(r'json:"([^",]+)', match.group(1)))
            self.assertEqual(tags, set(SCHEMAS[component]['properties']), typename)

    def test_retired_unversioned_writes_match_real_handlers(self):
        route = DOC['paths']['/api/v1/admin/users/{id}/permissions']
        handlers = (ROOT / 'internal/api/handlers/permissions.go').read_text()
        for method, handler in (('put', 'SetUserPermissionOverride'), ('delete', 'DeleteUserPermissionOverride')):
            op = route[method]
            self.assertTrue(op['deprecated'])
            self.assertNotIn('requestBody', op)
            self.assertNotIn('200', op['responses'])
            self.assertNotIn('204', op['responses'])
            self.assertIn('409', op['responses'])
            body = re.search(r'func \(h \*PermissionHandler\) ' + handler + r'\([^)]*\) \{(.*?)\n\}', handlers, re.S)
            self.assertIsNotNone(body, handler)
            self.assertIn('errConflict(', body.group(1))
            self.assertNotIn('h.service.', body.group(1))

    def test_raw_domain_presence_and_null_are_not_list(self):
        raw = SCHEMAS['PermissionEditorRawDomainAccess']
        self.assertEqual(set(raw['required']), {'mode', 'zone_ids'})
        for mode in ('inherit', 'all', 'none'):
            for zones in (None, []):
                self.assertTrue(accepts(raw, dict(mode=mode, zone_ids=zones)))
        self.assertFalse(accepts(raw, dict(mode='none')))
        self.assertFalse(accepts(raw, dict(mode='list', zone_ids=None)))
        self.assertFalse(accepts(raw, dict(mode='list', zone_ids=[])))
        self.assertTrue(accepts(raw, dict(mode='list', zone_ids=[U])))
        self.assertTrue(accepts(SCHEMAS['PermissionEditorRawOverrides']['properties']['allowed_zone_ids'], None))

    def test_assignment_intent_requires_both_observations_and_patch(self):
        schema = SCHEMAS['PermissionEditorAssignmentCommand']
        command = dict(expected_revision=REV, profile_id=None, profile_revision=None, patch={})
        self.assertTrue(accepts(schema, command))
        self.assertTrue(accepts(schema, {**command, 'profile_id': U, 'profile_revision': '7', 'patch': {'can_send': False}}))
        for key in command:
            bad = dict(command)
            bad.pop(key)
            self.assertFalse(accepts(schema, bad), key)
        for change in ({'profile_id': U}, {'profile_revision': '7'}, {'profile_id': U, 'profile_revision': '0'}, {'unknown': True}):
            self.assertFalse(accepts(schema, {**command, **change}))

    def test_profile_preview_confirmation_and_update_shape(self):
        preview = SCHEMAS['PermissionProfileDeletionPreview']
        self.assertEqual(set(preview['required']), {'profile_id', 'profile_revision', 'members', 'changes'})
        self.assertEqual(set(SCHEMAS['PermissionProfileDeletionChange']['required']), {'revision', 'before', 'after'})
        delete = SCHEMAS['PermissionProfileDeleteCommand']
        self.assertTrue(accepts(delete, dict(expected_revision='1', confirmed_members=[])))
        self.assertTrue(accepts(delete, dict(expected_revision='1', confirmed_members=[REV])))
        for body in ({}, {'expected_revision': '1'}, {'expected_revision': '1', 'confirmed_members': None}, {'expected_revision': 1, 'confirmed_members': []}):
            self.assertFalse(accepts(delete, body))
        source = (ROOT / 'internal/app/permissions/service.go').read_text()
        match = re.search(r'type UpdateInput struct \{(.*?)\n\}', source, re.S)
        tags = set(re.findall(r'json:"([^",]+)', match.group(1)))
        update = SCHEMAS['PermissionProfileUpdateCommand']
        self.assertEqual(tags, set(update['properties']))
        self.assertEqual(update['required'], ['expected_revision'])
        for change in ({'can_send': False}, {'can_send': None}, {'daily_send_quota': 0}, {'allowed_zone_ids': []}, {'allowed_zone_ids': None}):
            self.assertTrue(accepts(update, {'expected_revision': '1', **change}))
        self.assertFalse(accepts(update, {'expected_revision': '1', 'daily_send_quota': -1}))

    def test_profile_and_assignment_routes_match_real_router(self):
        router = (ROOT / 'internal/api/router.go').read_text()
        bindings = (
            ('get', '/api/v1/admin/permissions/{id}/deletion-preview', 'ProfileDeletionPreview', None, 'PermissionProfileDeletionPreviewResponse', '200'),
            ('post', '/api/v1/admin/users/{id}/permission-editor/assignment', 'AssignPermissionEditor', 'PermissionEditorAssignmentCommand', 'PermissionEditorResponse', '200'),
            ('patch', '/api/v1/admin/permissions/{id}', 'UpdateProfile', 'PermissionProfileUpdateCommand', None, '200'),
            ('delete', '/api/v1/admin/permissions/{id}', 'DeleteProfile', 'PermissionProfileDeleteCommand', None, '204'),
        )
        for method, path, handler, request, response, status in bindings:
            self.assertIn('r.' + method.title() + '("' + path.removeprefix('/api/v1') + '", perm.' + handler + ')', router)
            operation = DOC['paths'][path][method]
            self.assertIn(status, operation['responses'])
            self.assertIn('409', operation['responses'])
            if request:
                self.assertEqual(operation['requestBody']['content']['application/json']['schema']['$ref'], '#/components/schemas/' + request)
            if response:
                self.assertEqual(operation['responses'][status]['content']['application/json']['schema']['$ref'], '#/components/schemas/' + response)
        handlers = (ROOT / 'internal/api/handlers/permissions.go').read_text()
        self.assertIn('json:"confirmed_members"', handlers)
        self.assertIn('body.ExpectedRevision, body.ConfirmedMembers', handlers)

    def test_schema_mutations_are_observable(self):
        patch = copy.deepcopy(SCHEMAS['PermissionEditorPatch'])
        patch['properties']['can_send'] = {'type': 'boolean'}
        self.assertFalse(accepts(patch, {'can_send': None}))
        patch = copy.deepcopy(SCHEMAS['PermissionEditorPatch'])
        patch['required'] = ['can_send']
        self.assertFalse(accepts(patch, {}))
        revision = copy.deepcopy(SCHEMAS['PermissionEditorRevision'])
        revision['properties']['user_revision'] = {'type': 'integer'}
        self.assertFalse(accepts(revision, REV))


if __name__ == '__main__':
    unittest.main()
