"""All company routes must have a response binding, including non-JSON paths."""
import copy
import unittest
from test_openapi_contract import gate, ROOT
import json


class CompleteResponseBindingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.document, errors = gate.parse_openapi_text((ROOT / 'internal/api/openapi.yaml').read_text())
        if errors:
            raise AssertionError(errors)
        cls.inventory = json.loads((ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
        cls.rows = [r for r in cls.inventory if r['path'].startswith('/api/v1/company/')]

    def test_every_handler_has_an_explicit_response_contract(self):
        bound = set(gate.COMPANY_RESPONSES) | set(getattr(gate, 'COMPANY_STREAMS', {}))
        self.assertEqual({r['handler'] for r in self.rows} - bound, set())

    def test_new_handler_cannot_silently_become_route_only(self):
        d = copy.deepcopy(self.document)
        rows = copy.deepcopy(self.inventory)
        row = next(r for r in rows if r['handler'] == 'c.Drafts.Delete')
        row['handler'] = 'c.NewUnreviewed.Delete'
        self.assertTrue(gate.check_company_operations(d, rows))

    def test_acknowledgement_requires_concrete_data_schema(self):
        for row in self.rows:
            if row['handler'] not in ('c.Drafts.Delete', 'c.Setup.Activate', 'c.Mailboxes.Grant',
                                      'c.Console.RetryIndex', 'c.Recovery.InspectOutbound',
                                      'c.Templates.Preview', 'c.Templates.TemplateGrants'):
                continue
            with self.subTest(handler=row['handler']):
                d = copy.deepcopy(self.document)
                schema = d['paths'][row['path']][row['method'].lower()]['responses']['200']['content']['application/json']['schema']
                schema['properties']['data'] = {}
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_replay_and_new_submission_are_both_checked(self):
        for status in ('200', '201'):
            with self.subTest(status=status):
                d = copy.deepcopy(self.document)
                del d['paths']['/api/v1/company/drafts/{id}/submit']['post']['responses'][status]
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_streams_require_correct_content_types(self):
        for path in ('/api/v1/company/mailboxes/{id}/events',
                     '/api/v1/company/mailboxes/{id}/messages/{message}/source',
                     '/api/v1/company/submissions/{id}/attachments/{aid}/download'):
            with self.subTest(path=path):
                d = copy.deepcopy(self.document)
                d['paths'][path]['get']['responses']['200']['content'] = {'application/json': {'schema': {}}}
                self.assertTrue(gate.check_company_operations(d, self.inventory))

    def test_original_mail_is_rfc822_not_generic_binary(self):
        op = self.document['paths']['/api/v1/company/mailboxes/{id}/messages/{message}/source']['get']
        self.assertIn('message/rfc822', op['responses']['200']['content'])

    def test_binary_safety_headers_are_mandatory(self):
        d = copy.deepcopy(self.document)
        response = d['paths']['/api/v1/company/attachments/{id}']['get']['responses']['200']
        response.pop('headers', None)
        self.assertTrue(gate.check_company_operations(d, self.inventory))


if __name__ == '__main__':
    unittest.main()
