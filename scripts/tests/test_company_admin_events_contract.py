"""Source/static OpenAPI SSE contract only; not runtime authorization proof."""
import importlib.util
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('admin_events_openapi_gate', ROOT / 'scripts/check_contract_drift.py')
gate = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = gate
spec.loader.exec_module(gate)


class CompanyAdminEventsContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.doc, errors = gate.parse_openapi_text((ROOT / 'internal/api/openapi.yaml').read_text())
        if errors:
            raise AssertionError(errors)
        cls.schemas = cls.doc['components']['schemas']
        cls.handler = (ROOT / 'internal/api/handlers/company_admin_stream.go').read_text()
        cls.consumer = (ROOT / 'web/lib/api/company-events.ts').read_text()

    def test_exact_wire_fields_match_both_producer_and_consumer(self):
        for component, typename, fields in (
            ('CompanyAdminInvalidation', 'companyAdminInvalidation', {'type', 'tenant_id', 'occurred_at', 'metadata'}),
            ('CompanyAdminEventMetadata', 'companyAdminMetadata', {'action', 'resource_type', 'resource_id'}),
        ):
            schema = self.schemas[component]
            self.assertEqual(set(schema['properties']), fields)
            self.assertEqual(set(schema['required']), fields)
            self.assertIs(schema['additionalProperties'], False)
            body = re.search(r'type ' + typename + r' struct \{(.*?)\n\}', self.handler, re.S).group(1)
            self.assertEqual(set(re.findall(r'json:"([^",]+)', body)), fields)
            for field in fields:
                self.assertIn('"' + field + '"', self.consumer)
        scope = self.schemas['CompanyAdminStreamScope']
        self.assertEqual(set(scope['properties']), {'tenant_id'})
        self.assertEqual(scope['required'], ['tenant_id'])
        self.assertIs(scope['additionalProperties'], False)

    def test_action_resource_allowlist_matches_server_and_consumer(self):
        mapping = dict(re.findall(r'"([a-z_.]+)": "([a-z_]+)"', self.consumer.split('export type')[0]))
        switch = self.handler.split('func companyAdminResourceType(', 1)[1].split('func projectCompanyAdminInvalidation', 1)[0]
        server = {}
        for actions, resource in re.findall(r'case (.*?):\s*return "([a-z_]+)"', switch):
            for action in re.findall(r'"([a-z_.]+)"', actions):
                server[action] = resource
        self.assertEqual(server, mapping)
        schema = self.schemas['CompanyAdminEventMetadata']
        self.assertEqual(set(schema['properties']['action']['enum']), set(mapping))
        documented = {}
        for variant in schema['oneOf']:
            props = variant['properties']
            for action in props['action']['enum']:
                documented[action] = props['resource_type']['const']
        self.assertEqual(documented, mapping)
        self.assertNotIn('message.changed', mapping)
        self.assertNotIn('sent.changed', mapping)

    def test_route_sse_frames_and_advisory_last_id(self):
        op = self.doc['paths']['/api/v1/company/events']['get']
        self.assertEqual(op['security'], [{'BearerAuth': []}])
        self.assertNotIn('requestBody', op)
        self.assertEqual(set(op['responses']['200']['content']), {'text/event-stream'})
        frames = op['x-sse-events']
        self.assertEqual(set(frames), {'ready', 'resync', 'company.admin.changed'})
        for kind in ('ready', 'resync'):
            self.assertEqual(frames[kind]['data']['$ref'], '#/components/schemas/CompanyAdminStreamScope')
            self.assertIn('frame("' + kind + '", "", scope)', self.handler)
        self.assertEqual(frames['company.admin.changed']['data']['$ref'], '#/components/schemas/CompanyAdminInvalidation')
        self.assertIn('frame("company.admin.changed", event.ID.String(), value)', self.handler)
        self.assertEqual(op['parameters'][0]['name'], 'Last-Event-ID')
        self.assertFalse(op['parameters'][0]['required'])
        self.assertIn('advisory', op['description'])
        self.assertIn('never a commit-ordered replay cursor', op['description'])
        for status in ('400', '401', '403', '500'):
            self.assertIn(status, op['responses'])
        router = (ROOT / 'internal/api/router.go').read_text()
        self.assertIn('r.With(middleware.RequireAuth, middleware.RequireAdmin).Get("/company/events", adminEvents.Events)', router)

    def test_read_and_frame_fences_are_source_evidence_not_broadcast(self):
        self.assertIn('initial.Type != authz.PrincipalUser', self.handler)
        self.assertIn('current.TenantID != initial.TenantID', self.handler)
        self.assertIn('!sameSession', self.handler)
        self.assertIn('WithCompanyAdminEventAccess(fresh.Context(), current', self.handler)
        self.assertIn('controller.Flush()', self.handler)
        store = (ROOT / 'internal/store/postgres/company_admin_events.go').read_text()
        self.assertIn('s.companyReadTx(ctx, actor, true', store)
        self.assertIn("payload->>'tenant_id'=$1", store)
        self.assertIn('LIMIT $2', store)
        self.assertNotIn('UPDATE outbox_events', store)
        self.assertNotIn('DELETE FROM outbox_events', store)


if __name__ == '__main__':
    unittest.main()
