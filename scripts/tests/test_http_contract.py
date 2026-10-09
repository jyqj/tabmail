"""Runtime schema validation must reject malformed evidence and wrong wire bytes."""
import base64
import copy
import importlib.util
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import check_http_contract as runtime


def document():
    return {
        'openapi': '3.1.0',
        'components': {'schemas': {'Receipt': {
            'type': 'object', 'required': ['id', 'created_at', 'items', 'updated'],
            'properties': {'id': {'type': 'string', 'format': 'uuid'},
                           'created_at': {'type': 'string', 'format': 'date-time'},
                           'items': {'type': 'array', 'items': {'type': 'integer'}},
                           'updated': {'type': 'boolean', 'const': True}},
            'additionalProperties': False}},
            'responses': {'Success': {'content': {'application/json': {'schema': {
                'type': 'object', 'required': ['data'], 'additionalProperties': False,
                'properties': {'data': {'$ref': '#/components/schemas/Receipt'}}}}}}}},
        'paths': {'/api/v1/company/example/{id}': {'get': {'responses': {
            '200': {'$ref': '#/components/responses/Success'}}}}},
    }


def record(value=None):
    if value is None:
        value = {'data': {'id': '11111111-1111-4111-8111-111111111111',
                          'created_at': '2026-09-29T00:00:00Z', 'items': [], 'updated': True}}
    return {'id': 'example', 'method': 'GET', 'path': '/api/v1/company/example/1',
            'route': '/api/v1/company/example/{id}', 'status': 200,
            'headers': {'Content-Type': 'application/json; charset=utf-8'},
            'body_base64': base64.b64encode(json.dumps(value).encode()).decode()}


def body(case):
    return json.loads(base64.b64decode(case['body_base64']))


def set_body(case, value):
    case['body_base64'] = base64.b64encode(json.dumps(value).encode()).decode()
    return case


class RuntimeResponseTests(unittest.TestCase):
    def setUp(self):
        self.validator = runtime.HTTPContractValidator(document())

    def test_resolves_local_response_and_nested_component_refs(self):
        self.assertEqual(self.validator.validate_response(record()), [])

    def test_nullable_collection_is_not_an_array(self):
        r = record(); v = body(r); v['data']['items'] = None
        self.assertTrue(self.validator.validate_response(set_body(r, v)))

    def test_false_zero_and_wrong_scalar_are_not_coerced(self):
        for value in (False, 0, 'true', None):
            r = record(); v = body(r); v['data']['updated'] = value
            self.assertTrue(self.validator.validate_response(set_body(r, v)))

    def test_missing_required_and_unexpected_field_fail(self):
        for mutation in ('delete', 'extra'):
            r = record(); v = body(r)
            if mutation == 'delete': del v['data']['id']
            else: v['data']['extra'] = 0
            self.assertTrue(self.validator.validate_response(set_body(r, v)))

    def test_real_format_checks_are_enabled(self):
        for field, value in (('id', 'not-a-uuid'), ('created_at', '2026-99-55T99:00:00Z')):
            r = record(); v = body(r); v['data'][field] = value
            self.assertTrue(self.validator.validate_response(set_body(r, v)))

    def test_status_method_route_and_content_type_are_checked(self):
        for field, value in (('status', 201), ('method', 'POST'), ('route', '/wrong/path'),
                             ('path', 'https://external.test/api/v1/company/example/1'),
                             ('path', '/api/v1/company/wrong/1')):
            r = record(); r[field] = value
            self.assertTrue(self.validator.validate_response(r))
        r = record(); r['headers']['Content-Type'] = 'text/html'
        self.assertTrue(self.validator.validate_response(r))

    def test_capture_types_and_base64_are_strict(self):
        for field, value in (('status', True), ('body_base64', '!'), ('headers', []),
                             ('id', []), ('route', None)):
            r = record(); r[field] = value
            self.assertTrue(self.validator.validate_response(r))
        r = record(); r['unexpected'] = True
        self.assertTrue(self.validator.validate_response(r))

    def test_json_duplicates_and_nonfinite_numbers_are_rejected(self):
        for text in ('{"data":1,"data":2}', '{"data":NaN}', 'not-json'):
            r = record(); r['body_base64'] = base64.b64encode(text.encode()).decode()
            self.assertTrue(self.validator.validate_response(r))

    def test_secret_fields_fail_even_in_permissive_old_components(self):
        d = document(); d['components']['schemas']['Receipt']['additionalProperties'] = True
        validator = runtime.HTTPContractValidator(d)
        for field in runtime.FORBIDDEN_FIELDS:
            r = record(); v = body(r); v['data'][field] = 'fixture-marker'
            errors = validator.validate_response(set_body(r, v))
            self.assertTrue(any(e.startswith('[http-secret]') for e in errors), errors)

    def test_remote_refs_are_rejected_before_any_network_access(self):
        d = document(); d['components']['schemas']['Receipt'] = {'$ref': 'https://external.test/schema'}
        with self.assertRaises(ValueError): runtime.HTTPContractValidator(d)

    def test_schema_error_messages_do_not_echo_response_content(self):
        r = record(); v = body(r); v['data']['updated'] = 'PRIVATE-FIXTURE-PAYLOAD'
        errors = self.validator.validate_response(set_body(r, v))
        self.assertTrue(errors)
        self.assertNotIn('PRIVATE-FIXTURE-PAYLOAD', repr(errors))

    def test_binary_headers_and_sse_framing(self):
        for media in ('application/octet-stream', 'message/rfc822', 'text/event-stream'):
            d = document(); op = d['paths']['/api/v1/company/example/{id}']['get']
            op['responses']['200'] = {'content': {media: {'schema': {'type': 'string'}}}}
            validator = runtime.HTTPContractValidator(d)
            r = record(); r['headers'] = {'Content-Type': media}
            r['body_base64'] = base64.b64encode(b'fixture').decode()
            self.assertTrue(validator.validate_response(r))
            r['headers'].update({'Cache-Control': 'private, no-store',
                'Content-Disposition': 'attachment; filename=fixture.txt',
                'X-Content-Type-Options': 'nosniff', 'X-Accel-Buffering': 'no'})
            if media == 'text/event-stream':
                r['body_base64'] = base64.b64encode(b'event: ready\ndata: {}\n\n').decode()
            self.assertEqual(validator.validate_response(r), [])


class AdditionalWireSafetyTests(unittest.TestCase):
    def test_numeric_overflow_cannot_create_nonfinite_json_values(self):
        for text in ('1e999', '-1e999', '{"items":[1e999]}'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                runtime.strict_json(text)
        self.assertEqual(runtime.strict_json('1.25e2'), 125.0)

    def test_public_cache_directive_cannot_hide_among_download_requirements(self):
        d = document()
        op = d['paths']['/api/v1/company/example/{id}']['get']
        op['responses']['200'] = {'content': {
            'application/octet-stream': {'schema': {'type': 'string', 'format': 'binary'}}}}
        validator = runtime.HTTPContractValidator(d)
        case = record()
        case['body_base64'] = base64.b64encode(b'fixture').decode()
        case['headers'] = {'Content-Type': 'application/octet-stream',
                           'Cache-Control': 'public, private, no-store',
                           'Content-Disposition': 'attachment; filename=fixture.txt',
                           'X-Content-Type-Options': 'nosniff'}
        self.assertTrue(any(e.startswith('[http-download]')
                            for e in validator.validate_response(case)))


class WireSafetyRegressionTests(unittest.TestCase):
    def test_json_exponent_overflow_is_not_finite_json(self):
        for number in ('1e400', '-1e400'):
            with self.subTest(number=number), self.assertRaises(ValueError):
                runtime.strict_json('{"value":' + number + '}')

    def test_sse_payload_cannot_bypass_secret_field_check(self):
        headers = {'cache-control': 'private, no-cache', 'x-accel-buffering': 'no'}
        for field in runtime.FORBIDDEN_FIELDS:
            raw = ('event: ready\ndata: ' + json.dumps({'nested': [{field: 'private-fixture-value'}]}) + '\n\n').encode()
            errors = runtime.tabmail_sse_errors(raw, headers, 'sse-secret')
            self.assertTrue(any(e.startswith('[http-secret]') for e in errors), field)
            self.assertNotIn('private-fixture-value', repr(errors))

    def test_binary_public_directive_cannot_override_private_no_store(self):
        d = document()
        d['paths']['/api/v1/company/example/{id}']['get']['responses']['200'] = {
            'content': {'application/octet-stream': {'schema': {'type': 'string'}}}}
        r = record()
        r['headers'] = {'Content-Type': 'application/octet-stream',
                        'Cache-Control': 'public, private, no-store',
                        'Content-Disposition': 'attachment; filename=fixture.txt',
                        'X-Content-Type-Options': 'nosniff'}
        r['body_base64'] = base64.b64encode(b'fixture').decode()
        self.assertTrue(runtime.HTTPContractValidator(d).validate_response(r))


class RuntimeEvidencePublicationTests(unittest.TestCase):
    def test_ci_does_not_upload_raw_http_responses(self):
        import yaml
        workflow = yaml.safe_load((ROOT / '.github/workflows/company-p0.yml').read_text())
        uploads = [s for s in workflow['jobs']['backend']['steps']
                   if s.get('uses', '').startswith('actions/upload-artifact@')]
        self.assertTrue(uploads)
        paths = '\n'.join(s.get('with', {}).get('path', '') for s in uploads).splitlines()
        self.assertIn('!http-contract-evidence/responses.json', paths)


class PrivateEvidenceDirectoryTests(unittest.TestCase):
    def test_driver_creates_private_evidence_even_with_permissive_umask(self):
        import contextlib
        import io
        import os
        import stat
        import tempfile
        from unittest.mock import patch

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            spec = root / 'fixture.yaml'
            spec.write_text('openapi: 3.1.0\n')
            output = root / 'evidence'
            old_umask = os.umask(0)
            try:
                with patch.object(runtime.sys, 'argv', [
                    'check_http_contract.py', '--output-dir', str(output),
                    '--source-sha', '0' * 40,
                ]), patch.object(runtime.contracts, 'OPENAPI_SPEC', spec), \
                        patch.object(runtime.contracts, 'parse_openapi_text',
                                     return_value=(None, ['synthetic stop before execution'])), \
                        patch.object(runtime, 'run_process_group') as run, \
                        contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(runtime.main(), 1)
                    run.assert_not_called()
            finally:
                os.umask(old_umask)
            self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o700)
            self.assertTrue((output / 'result.json').is_file())


class CaptureBoundTests(unittest.TestCase):
    def test_encoded_body_is_rejected_before_allocation(self):
        from unittest.mock import patch
        case = record()
        case['body_base64'] = 'A' * (4 * ((runtime.MAX_BODY_BYTES + 2) // 3) + 4)
        validator = runtime.HTTPContractValidator(document())
        with patch.object(runtime.base64, 'b64decode', side_effect=AssertionError('oversized input decoded')):
            self.assertTrue(validator.validate_response(case))

    def test_nested_json_is_bounded_before_schema_evaluation(self):
        with self.assertRaises(ValueError):
            runtime.strict_json('[' * 80 + '0' + ']' * 80)
        self.assertEqual(runtime.strict_json('{"value":[{"nested":1}]}'), {'value': [{'nested': 1}]})


class CaptureCompletenessTests(unittest.TestCase):
    def test_absent_stale_empty_or_wrong_producer_capture_fails(self):
        captures = [None, {}, {'version': 1, 'producer': 'Wrong', 'spec_sha256': 'hash', 'cases': [record()]},
                    {'version': 1, 'producer': 'TestCompanyHTTPContract', 'spec_sha256': 'stale', 'cases': [record()]},
                    {'version': 1, 'producer': 'TestCompanyHTTPContract', 'spec_sha256': 'hash', 'cases': []}]
        for capture in captures:
            self.assertEqual(runtime.validate_capture(document(), capture, 'hash', [])['status'], 'fail')

    def test_missing_cases_and_duplicate_ids_fail(self):
        # Even a matching schema hash is insufficient without executed cases.
        capture = {'version': 1, 'producer': 'TestCompanyHTTPContract', 'spec_sha256': 'hash', 'cases': [record(), record()]}
        inventory = [{'method': 'GET', 'path': '/api/v1/company/example/{id}',
                      'handler': 'c.Drafts.Get'}]
        result = runtime.validate_capture(document(), capture, 'hash', inventory)
        self.assertEqual(result['status'], 'fail')
        self.assertTrue(any('duplicate' in e for e in result['errors']))
        self.assertTrue(any('[http-missing-case]' in e for e in result['errors']))


class ScenarioEvidenceTests(unittest.TestCase):
    def setUp(self):
        # Isolate evidence bookkeeping. Actual Draft202012Validator behavior is
        # exercised by RuntimeResponseTests; valid schemas cannot prove coverage.
        from unittest.mock import patch
        validator = patch.object(runtime, 'HTTPContractValidator')
        self.addCleanup(validator.stop)
        validator.start().return_value.validate_response.return_value = []

    def forged_capture(self):
        # Valid JSON is not proof that each named scenario actually executed.
        cases = []
        for name in sorted(runtime.REQUIRED_CASES):
            case = record()
            case['id'] = name
            cases.append(case)
        return {'version': 1, 'producer': 'TestCompanyHTTPContract',
                'spec_sha256': 'hash', 'cases': cases}

    def inventory(self):
        return [{'method': 'GET', 'path': '/api/v1/company/example/{id}',
                 'handler': 'c.Drafts.Get'}]

    def test_complete_matching_scenario_can_pass(self):
        from unittest.mock import patch
        capture = {'version': 1, 'producer': 'TestCompanyHTTPContract',
                   'spec_sha256': 'hash', 'cases': [record()]}
        with patch.object(runtime, 'REQUIRED_CASES', {
                'example': ('GET', '/api/v1/company/example/{id}', 200)}):
            result = runtime.validate_capture(document(), capture, 'hash', self.inventory())
        self.assertEqual(result['status'], 'pass')
        self.assertEqual(result['successful_operations'], 1)

    def test_required_names_cannot_substitute_for_status_and_route(self):
        result = runtime.validate_capture(document(), self.forged_capture(), 'hash', self.inventory())
        self.assertEqual(result['status'], 'fail')
        self.assertTrue(any('[http-case-mismatch]' in e for e in result['errors']))

    def test_empty_or_non_company_inventory_cannot_pass(self):
        for inventory in ([], [{'method': 'GET', 'path': '/health', 'handler': 'health'}]):
            result = runtime.validate_capture(document(), self.forged_capture(), 'hash', inventory)
            self.assertEqual(result['status'], 'fail')
            self.assertTrue(any('[http-inventory]' in e for e in result['errors']))

    def test_malformed_inventory_returns_a_failed_report(self):
        for inventory in (None, {}, [None], [{'method': 'GET'}]):
            result = runtime.validate_capture(document(), self.forged_capture(), 'hash', inventory)
            self.assertEqual(result['status'], 'fail')
            self.assertTrue(any('[http-inventory]' in e for e in result['errors']))

    def test_duplicate_or_unknown_handler_inventory_is_rejected(self):
        unknown = self.inventory()
        unknown[0]['handler'] = 'c.Unreviewed.New'
        for inventory in (self.inventory() * 2, unknown):
            result = runtime.validate_capture(document(), self.forged_capture(), 'hash', inventory)
            self.assertEqual(result['status'], 'fail')
            self.assertTrue(any('[http-inventory]' in e for e in result['errors']))


class RuntimeSSETests(unittest.TestCase):
    def validate(self, payload, cache='private, no-cache, no-transform'):
        # SSE has no JSON Schema instance evaluation. Exercise the actual wire
        # checks without importing optional JSON validator dependencies here.
        d = document()
        d['paths']['/api/v1/company/example/{id}']['get']['responses']['200'] = {
            'content': {'text/event-stream': {'schema': {'type': 'string'}}}}
        validator = object.__new__(runtime.HTTPContractValidator)
        validator.document = d
        case = record()
        case['headers'] = {'Content-Type': 'text/event-stream',
                           'Cache-Control': cache, 'X-Accel-Buffering': 'no'}
        case['body_base64'] = base64.b64encode(payload).decode()
        return validator.validate_response(case)

    def test_malformed_or_comment_only_sse_is_not_an_event(self):
        for payload in (b'data: not-json\n\n', b': data: {}\n\n',
                        b'data: {"x":1,"x":2}\n\n', b'data: null\n\n'):
            self.assertTrue(self.validate(payload))

    def test_sse_cache_directives_are_tokens_not_substrings(self):
        for cache in ('not-private, no-cache', 'public, private, no-cache'):
            self.assertTrue(self.validate(b'data: {}\n\n', cache))

    def test_complete_json_event_accepts_lf_and_crlf(self):
        for payload in (b'event: ready\ndata: {}\n\n',
                        b'event: ready\r\ndata: {}\r\n\r\n'):
            self.assertEqual(self.validate(payload), [])
        self.assertTrue(self.validate(b'event: ready\ndata: {}\n'))


class CompanyAdminStreamCaptureTests(unittest.TestCase):
    def setUp(self):
        self.admin_path = '/api/v1/company/events'
        self.mailbox_path = '/api/v1/company/mailboxes/{id}/events'
        self.required = {
            name: runtime.REQUIRED_CASES[name]
            for name in ('company.events.ready', 'events.ready')
        }
        self.assertEqual(self.required['company.events.ready'], ('GET', self.admin_path, 200))
        self.assertEqual(runtime.contracts.COMPANY_STREAMS['adminEvents.Events'], 'text/event-stream')
        self.document = {'openapi': '3.1.0', 'paths': {
            path: {'get': {'responses': {'200': {'content': {
                'text/event-stream': {'schema': {'type': 'string'}}}}}}}
            for path in (self.admin_path, self.mailbox_path)
        }}
        self.inventory = [
            {'method': 'GET', 'path': self.admin_path, 'handler': 'adminEvents.Events'},
            {'method': 'GET', 'path': self.mailbox_path, 'handler': 'c.Events.Events'},
        ]

    def capture(self):
        cases = []
        for name, (_, route, _) in self.required.items():
            cases.append({
                'id': name, 'method': 'GET', 'route': route,
                'path': route.replace('{id}', '11111111-1111-4111-8111-111111111111'),
                'status': 200,
                'headers': {'Content-Type': 'text/event-stream',
                            'Cache-Control': 'private, no-store, no-transform',
                            'X-Accel-Buffering': 'no'},
                'body_base64': base64.b64encode(
                    b'event: ready\ndata: {"tenant_id":"11111111-1111-4111-8111-111111111111"}\n\n'
                ).decode(),
            })
        return {'version': 1, 'producer': 'TestCompanyHTTPContract',
                'spec_sha256': 'hash', 'cases': cases}

    def validate(self, capture):
        from unittest.mock import patch
        # Only isolate unrelated required scenarios; use the real inventory,
        # response/media validator and SSE framing/secret checks throughout.
        with patch.object(runtime, 'REQUIRED_CASES', self.required):
            return runtime.validate_capture(self.document, capture, 'hash', self.inventory)

    def test_distinct_admin_and_mailbox_sse_captures_pass(self):
        result = self.validate(self.capture())
        self.assertEqual(result['status'], 'pass', result['errors'])
        self.assertEqual(result['successful_operations'], 2)

    def test_mailbox_stream_cannot_cover_missing_admin_capture(self):
        capture = self.capture()
        capture['cases'] = [c for c in capture['cases'] if c['id'] != 'company.events.ready']
        result = self.validate(capture)
        self.assertEqual(result['status'], 'fail')
        self.assertIn('[http-missing-case] company.events.ready', result['errors'])
        self.assertTrue(any('[http-missing-success]' in e and self.admin_path in e
                            for e in result['errors']), result['errors'])

    def test_admin_name_on_mailbox_route_cannot_cover_admin_operation(self):
        capture = self.capture()
        admin, mailbox = capture['cases']
        admin['path'], admin['route'] = mailbox['path'], mailbox['route']
        result = self.validate(capture)
        self.assertEqual(result['status'], 'fail')
        self.assertTrue(any('[http-case-mismatch] company.events.ready' in e
                            for e in result['errors']), result['errors'])

    def test_admin_json_response_cannot_replace_sse_media(self):
        capture = self.capture()
        capture['cases'][0]['headers']['Content-Type'] = 'application/json'
        capture['cases'][0]['body_base64'] = base64.b64encode(b'{"data":{}}').decode()
        result = self.validate(capture)
        self.assertEqual(result['status'], 'fail')
        self.assertTrue(any('[http-content-type] company.events.ready' in e
                            for e in result['errors']), result['errors'])

    def test_admin_missing_or_unterminated_frame_is_not_execution(self):
        for payload in (b'', b': keepalive\n\n', b'event: ready\ndata: {}\n'):
            with self.subTest(payload=payload):
                capture = self.capture()
                capture['cases'][0]['body_base64'] = base64.b64encode(payload).decode()
                result = self.validate(capture)
                self.assertEqual(result['status'], 'fail')
                self.assertTrue(any('[http-stream] company.events.ready' in e
                                    for e in result['errors']), result['errors'])


class RecipientsAggregateContractTests(unittest.TestCase):
    route = '/api/v1/company/outbound/{id}/recipients'

    @classmethod
    def setUpClass(cls):
        cls.document, errors = runtime.contracts.parse_openapi_text(
            (ROOT / 'internal/api/openapi.yaml').read_text())
        if errors:
            raise AssertionError(errors)
        cls.validator = runtime.HTTPContractValidator(cls.document)

    def receipt(self, known=True):
        progress = {'completeness': 'unknown'}
        if known:
            progress = {'completeness': 'known', 'counts': {
                'total': 3, 'accepted': 1, 'pending': 1,
                'temporary': 0, 'permanent': 1, 'uncertain': 0,
            }}
        return {'id': '11111111-1111-4111-8111-111111111111',
                'state': 'failed', 'status': 'needs_attention',
                'progress': progress, 'delivery_uncertain': False,
                'capabilities': {'view_content': True, 'retry': False,
                                 'retry_block_reason': 'sender_authority'}}

    def validate(self, data):
        case = {'id': 'recipients.list', 'method': 'GET', 'route': self.route,
                'path': self.route.replace('{id}', '11111111-1111-4111-8111-111111111111'),
                'status': 200, 'headers': {'Content-Type': 'application/json; charset=utf-8',
                                         'Cache-Control': 'private, no-store'},
                'body_base64': base64.b64encode(json.dumps({'data': data}).encode()).decode()}
        return self.validator.validate_response(case)

    def test_actual_operation_and_binding_are_one_existing_strict_receipt(self):
        self.assertEqual(runtime.contracts.COMPANY_RESPONSES['c.Recovery.Recipients'],
                         ('200', 'one', 'OutboundReceipt'))
        envelope = self.document['paths'][self.route]['get']['responses']['200'][
            'content']['application/json']['schema']
        self.assertEqual(envelope['required'], ['data'])
        self.assertIs(envelope['additionalProperties'], False)
        self.assertEqual(envelope['properties'], {
            'data': {'$ref': '#/components/schemas/OutboundReceipt'}})

    def test_known_complete_counts_and_unknown_without_counts_pass(self):
        for known in (True, False):
            with self.subTest(known=known):
                self.assertEqual(self.validate(self.receipt(known)), [])

    def test_old_per_address_array_is_not_an_aggregate_receipt(self):
        for rows in ([], [{'address': 'private@fixture.test', 'state': 'pending',
                          'smtp_code': 0, 'attempts': 0,
                          'updated_at': '2026-10-02T00:00:00Z'}]):
            with self.subTest(rows=bool(rows)):
                self.assertTrue(self.validate(rows))

    def test_known_missing_counts_and_unknown_fabricated_counts_fail(self):
        for progress in ({'completeness': 'known'},
                         {'completeness': 'unknown', 'counts': self.receipt()['progress']['counts']},
                         {'completeness': 'unknown', 'counts': None}):
            with self.subTest(progress=progress):
                value = self.receipt()
                value['progress'] = progress
                self.assertTrue(self.validate(value))

    def test_content_authority_does_not_admit_sensitive_or_unknown_fields(self):
        fields = {'address', 'to', 'cc', 'bcc', 'rcpt_to', 'recipients', 'subject',
                  'from', 'mail_from', 'text_body', 'html_body', 'headers', 'headers_json',
                  'smtp_code', 'smtp_response', 'diagnostic', 'last_error',
                  'raw_object_key', 'object_key', 'raw_mime', 'delivery_token',
                  'lease_until', 'content_redacted', 'unknown_future_field'}
        for field in fields:
            with self.subTest(field=field):
                value = self.receipt()
                self.assertTrue(value['capabilities']['view_content'])
                value[field] = 'PRIVATE-AGGREGATE-CANARY'
                errors = self.validate(value)
                self.assertTrue(errors)
                self.assertNotIn('PRIVATE-AGGREGATE-CANARY', repr(errors))

    def test_nested_progress_counts_and_capabilities_remain_closed(self):
        for path in (('progress',), ('progress', 'counts'), ('capabilities',)):
            for key in ('address', 'bcc', 'diagnostic', 'object_key', 'unknown_future_field'):
                with self.subTest(path=path, key=key):
                    value = self.receipt()
                    node = value
                    for name in path:
                        node = node[name]
                    node[key] = 'PRIVATE-NESTED-CANARY'
                    errors = self.validate(value)
                    self.assertTrue(errors)
                    self.assertNotIn('PRIVATE-NESTED-CANARY', repr(errors))

    def test_counts_keep_complete_nonnegative_integer_shape(self):
        for name in self.receipt()['progress']['counts']:
            with self.subTest(missing=name):
                value = self.receipt()
                del value['progress']['counts'][name]
                self.assertTrue(self.validate(value))
        for name, number in (('total', 0), ('pending', -1), ('accepted', True), ('uncertain', None)):
            with self.subTest(name=name, number=number):
                value = self.receipt()
                value['progress']['counts'][name] = number
                self.assertTrue(self.validate(value))


class ProcessLifetimeTests(unittest.TestCase):
    def test_timeout_terminates_the_child_process_group(self):
        import subprocess
        from unittest.mock import Mock, patch
        proc = Mock(pid=12345)
        proc.wait.side_effect = [subprocess.TimeoutExpired(['go'], 1), -15, -15]
        with patch.object(runtime.subprocess, 'Popen', return_value=proc) as spawn, patch.object(runtime.os, 'killpg') as kill:
            with self.assertRaises(subprocess.TimeoutExpired):
                runtime.run_process_group(['go'], cwd=ROOT, env={}, stdout=None, stderr=None, timeout=1)
        self.assertTrue(spawn.call_args.kwargs['start_new_session'])
        self.assertEqual(kill.call_args_list[0].args, (12345, runtime.signal.SIGTERM))
        self.assertEqual(kill.call_args_list[1].args, (12345, runtime.signal.SIGKILL))

    def test_normal_process_preserves_actual_exit_without_signals(self):
        from unittest.mock import Mock, patch
        proc = Mock(pid=12345)
        proc.wait.return_value = 7
        with patch.object(runtime.subprocess, 'Popen', return_value=proc), patch.object(runtime.os, 'killpg') as kill:
            result = runtime.run_process_group(['go'], cwd=ROOT, env={}, stdout=None, stderr=None, timeout=1)
        self.assertEqual(result.returncode, 7)
        kill.assert_not_called()


if __name__ == '__main__':
    unittest.main()
