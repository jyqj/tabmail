#!/usr/bin/env python3
"""Capture real Go/PG HTTP responses and validate the existing OpenAPI document.

This is the runtime companion to check_contract_drift.py, not another schema
engine. jsonschema's Draft202012Validator owns JSON Schema evaluation, and its
in-memory referencing.Registry has no network/filesystem retrieval callback.
See https://python-jsonschema.readthedocs.io/en/stable/referencing/ .
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import math
from pathlib import Path
import re
import subprocess
import os
import signal
import sys
from urllib.parse import urlsplit

import check_contract_drift as contracts

ROOT = Path(__file__).resolve().parents[1]
SPEC_URI = 'urn:tabmail:openapi:http-contract'
MAX_CAPTURE_BYTES = 16 * 1024 * 1024
MAX_BODY_BYTES = 2 * 1024 * 1024
# These exclusions are explicit test scope, not implicit success. A future
# loopback DNS fixture can close them without touching the validator itself.
LIVE_DNS_EXCLUSIONS = {
    'c.Domains.Verify': 'Live DNS verification is outside this loopback HTTP/PG fixture.',
    'c.Domains.Verification': 'Live DNS status lookups are outside this loopback HTTP/PG fixture.',
}
# Fixture coverage assertions, not another response schema: a name alone is not
# proof that the intended HTTP operation and status were observed.
BASE = '/api/v1/company'
REQUIRED_CASES = {
    'settings.unconfigured': ('GET', BASE + '/settings', 200),
    'draft.incomplete': ('POST', BASE + '/drafts', 200),
    'submit.created': ('POST', BASE + '/drafts/{id}/submit', 201),
    'submit.replayed': ('POST', BASE + '/drafts/{id}/submit', 200),
    'message.source': ('GET', BASE + '/mailboxes/{id}/messages/{message}/source', 200),
    'message.attachment.index': ('GET', BASE + '/mailboxes/{id}/messages/{message}/attachments/{index}', 200),
    'message.attachment.id': ('GET', BASE + '/mailboxes/{id}/messages/{message}/parts/{attachment}', 200),
    'submissions.download': ('GET', BASE + '/submissions/{id}/attachments/{aid}/download', 200),
    'events.ready': ('GET', BASE + '/mailboxes/{id}/events', 200),
    'company.events.ready': ('GET', BASE + '/events', 200),
    'archive.change': ('POST', BASE + '/mailboxes/{id}/sent/{message}/actions', 200),
    'recovery.inspect': ('POST', BASE + '/recovery/{id}/inspect', 200),
    'recovery.retry': ('POST', BASE + '/recovery/{id}/retry', 200),
    'outbound.inspect': ('POST', BASE + '/outbound/{id}/inspect', 200),
    'outbound.reconcile': ('POST', BASE + '/outbound/{id}/reconcile', 200),
    'offboard.execute': ('POST', BASE + '/employees/{id}/offboard', 200),
    'policy.conflict': ('PUT', BASE + '/mailboxes/{id}/send-policy', 409),
    'error.unauthenticated': ('GET', BASE + '/submissions', 401),
    'error.forbidden': ('GET', BASE + '/domains', 403),
    'error.bad-request': ('GET', BASE + '/drafts/{id}', 400),
    'error.not-found': ('GET', BASE + '/submissions/{id}/content', 404),
    'error.internal': ('GET', BASE + '/settings', 500),
}
FORBIDDEN_FIELDS = {
    'raw_mime', 'delivery_token', 'password_hash', 'token_hash',
    'refresh_token', 'access_token', 'dkim_private_key',
    'raw_object_key', 'object_key', 'raw_key', 'raw_hash',
}


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def strict_json(text: str):
    def pairs(items):
        out = {}
        for key, value in items:
            if key in out:
                raise ValueError('duplicate JSON field')
            out[key] = value
        return out
    def constant(_):
        raise ValueError('non-finite JSON number')
    def finite_float(value):
        number = float(value)
        if not math.isfinite(number):
            raise ValueError('JSON number exceeds finite float range')
        return number
    try:
        decoded = json.loads(text, object_pairs_hook=pairs, parse_constant=constant,
                             parse_float=finite_float)
    except RecursionError as exc:
        raise ValueError('JSON nesting exceeds parser limits') from exc
    pending = [(decoded, 0)]
    while pending:
        node, depth = pending.pop()
        if depth > 64:
            raise ValueError('JSON nesting exceeds 64 levels')
        if isinstance(node, dict):
            pending.extend((value, depth + 1) for value in node.values())
        elif isinstance(node, list):
            pending.extend((value, depth + 1) for value in node)
    return decoded


def secret_field_errors(value, label: str) -> list[str]:
    """Apply the same field deny-list to JSON envelopes and SSE JSON payloads.

    Walk iteratively so nesting does not add another recursion boundary. Error
    text identifies field names only, never serializes a response value.
    """
    errors = []
    pending = [value]
    while pending:
        node = pending.pop()
        if isinstance(node, dict):
            for key, child in node.items():
                if key.lower() in FORBIDDEN_FIELDS:
                    errors.append(f'[http-secret] {label}: forbidden field {key}')
                pending.append(child)
        elif isinstance(node, list):
            pending.extend(node)
    return errors


def tabmail_sse_errors(raw: bytes, headers: dict[str, str], label: str) -> list[str]:
    """Check captured TabMail JSON events, not a general-purpose SSE parser.

    Only complete blank-line-terminated frames count. Comments and unrelated
    fields cannot masquerade as data. JSON objects are TabMail's application
    contract; SSE itself does not require its payload to be JSON.
    """
    errors = []
    cache = {part.strip().lower() for part in headers.get('cache-control', '').split(',')}
    if headers.get('x-accel-buffering') != 'no' or 'private' not in cache or 'public' in cache:
        errors.append(f'[http-stream] {label}: missing private non-buffered stream headers')
    try:
        text = raw.decode('utf-8-sig').replace('\r\n', '\n').replace('\r', '\n')
    except UnicodeError:
        return errors + [f'[http-stream] {label}: invalid UTF-8 event stream']
    complete_events = 0
    for frame in text.split('\n\n')[:-1]:
        data = []
        for line in frame.split('\n'):
            field, separator, value = line.partition(':')
            if field == 'data':
                data.append(value[1:] if separator and value.startswith(' ') else value)
        if not data:
            continue
        try:
            value = strict_json('\n'.join(data))
            if not isinstance(value, dict):
                raise ValueError('TabMail event must be an object')
        except (ValueError, RecursionError):
            errors.append(f'[http-stream] {label}: malformed TabMail event payload')
            continue
        errors += secret_field_errors(value, label)
        complete_events += 1
    if not complete_events:
        errors.append(f'[http-stream] {label}: no complete TabMail event captured')
    return errors


class HTTPContractValidator:
    def __init__(self, document: dict):
        try:
            from jsonschema import Draft202012Validator, FormatChecker
            from referencing import Registry
            from referencing.jsonschema import DRAFT202012
        except ImportError as exc:
            raise RuntimeError('install scripts/requirements-contract.txt; runtime validation cannot be skipped') from exc
        errors = contracts._check_local_openapi_refs(document)
        if errors:
            raise ValueError('; '.join(errors))
        self.document = document
        self.validator_type = Draft202012Validator
        self.registry = Registry().with_resource(SPEC_URI, DRAFT202012.create_resource(document))
        self.formats = FormatChecker()
        # Missing optional format packages must not silently disable date-time.
        for name in ('uuid', 'date-time'):
            if name not in self.formats.checkers:
                raise RuntimeError(f'missing required format validator: {name}')
        for schema in contracts._object_at(document, 'components', 'schemas').values():
            Draft202012Validator.check_schema(schema)

    def resolve(self, value: dict) -> dict:
        seen = set()
        while isinstance(value, dict) and '$ref' in value:
            ref = value['$ref']
            if ref in seen or not isinstance(ref, str) or not ref.startswith('#/'):
                raise ValueError('cyclic or non-local response reference')
            seen.add(ref)
            value = self.registry.resolver().lookup(SPEC_URI + ref).contents
        if not isinstance(value, dict):
            raise ValueError('response must be an object')
        return value

    def schema_errors(self, schema: dict, instance, label: str) -> list[str]:
        self.validator_type.check_schema(schema)
        # Local refs are rooted at the complete OpenAPI document, including
        # nested allOf/anyOf. This does not dereference or simplify the schema.
        wrapper = {'$id': SPEC_URI, **schema, 'components': self.document.get('components', {})}
        validator = self.validator_type(wrapper, registry=self.registry, format_checker=self.formats)
        return [f'[http-schema] {label}: path={list(e.absolute_path)} keyword={e.validator}'
                for e in validator.iter_errors(instance)]

    def validate_response(self, case: dict) -> list[str]:
        if (not isinstance(case, dict) or set(case) != {'id', 'method', 'path', 'route', 'status', 'headers', 'body_base64'}
                or any(not isinstance(case.get(k), str) for k in ('id', 'method', 'path', 'route', 'body_base64'))
                or type(case.get('status')) is not int or not 100 <= case['status'] <= 599
                or not isinstance(case.get('headers'), dict)
                or any(not isinstance(k, str) or not isinstance(v, str) for k, v in case['headers'].items())):
            return ['[http-capture] malformed response record']
        label = case['id']
        url = urlsplit(case['path'])
        if url.scheme or url.netloc or url.fragment:
            return [f'[http-route] {label}: only relative application paths are accepted']
        expected, actual = case['route'].split('/'), url.path.split('/')
        if (len(expected) != len(actual) or any(e != a and not (e.startswith('{') and e.endswith('}') and a)
                for e, a in zip(expected, actual))):
            return [f'[http-route] {label}: route does not match actual request path']
        op = contracts._object_at(self.document, 'paths', case['route'], case['method'].lower())
        response = contracts._object_at(op, 'responses', str(case['status']))
        if not response:
            return [f'[http-status] {label}: undocumented operation/status']
        response = self.resolve(response)
        headers = {k.lower(): v for k, v in case['headers'].items()}
        if len(headers) != len(case['headers']):
            return [f'[http-header] {label}: ambiguous duplicate header names']
        media_type = headers.get('content-type', '').split(';', 1)[0].strip().lower()
        media = contracts._object_at(response, 'content', media_type)
        if not media or not isinstance(media.get('schema'), dict) or not media['schema']:
            return [f'[http-content-type] {label}: missing or undocumented media schema {media_type!r}']
        if len(case['body_base64']) > 4 * ((MAX_BODY_BYTES + 2) // 3):
            return [f'[http-capture] {label}: oversized encoded response']
        try:
            raw = base64.b64decode(case['body_base64'], validate=True)
        except (ValueError, TypeError):
            return [f'[http-capture] {label}: invalid base64 body']
        if len(raw) > MAX_BODY_BYTES:
            return [f'[http-capture] {label}: oversized response']
        errors = []
        for name, definition in contracts._object_at(response, 'headers').items():
            definition = self.resolve(definition)
            value = headers.get(name.lower())
            if value is None:
                if definition.get('required') is True:
                    errors.append(f'[http-header] {label}: required {name} missing')
            else:
                errors += self.schema_errors(definition.get('schema', {}), value, label + '.' + name)
        if media_type == 'application/json':
            try:
                value = strict_json(raw.decode('utf-8'))
            except (ValueError, UnicodeError, RecursionError):
                return errors + [f'[http-json] {label}: malformed JSON response']
            errors += self.schema_errors(media['schema'], value, label)
            errors += secret_field_errors(value, label)
        elif media_type == 'text/event-stream':
            errors += tabmail_sse_errors(raw, headers, label)
        elif media_type in ('application/octet-stream', 'message/rfc822'):
            cache = {x.strip().lower() for x in headers.get('cache-control', '').split(',')}
            if ('public' in cache or not {'private', 'no-store'} <= cache
                    or headers.get('x-content-type-options') != 'nosniff'):
                errors.append(f'[http-download] {label}: unsafe cache/sniff headers')
            if not headers.get('content-disposition', '').lower().startswith('attachment;'):
                errors.append(f'[http-download] {label}: forced attachment disposition missing')
            # Exact synthetic byte equality is asserted by the Go producer.
            if not raw:
                errors.append(f'[http-download] {label}: empty synthetic download')
        else:
            errors.append(f'[http-content-type] {label}: unsupported response media type')
        return errors


def company_inventory(inventory: object) -> tuple[list[dict], list[str]]:
    """Reject missing/ambiguous coverage inputs before counting any successes."""
    if not isinstance(inventory, list) or not inventory:
        return [], ['[http-inventory] a nonempty route inventory is required']
    rows, errors, seen = [], [], set()
    known = set(contracts.COMPANY_RESPONSES) | set(contracts.COMPANY_STREAMS)
    for row in inventory:
        if not isinstance(row, dict) or any(not isinstance(row.get(k), str) for k in ('method', 'path', 'handler')):
            return [], ['[http-inventory] malformed route record']
        if not row['path'].startswith(BASE + '/'):
            continue
        key = (row['method'], row['path'])
        if row['method'] not in {'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'} or key in seen:
            errors.append('[http-inventory] duplicate or invalid company operation')
        if row['handler'] not in known:
            errors.append('[http-inventory] company handler has no reviewed response binding')
        seen.add(key)
        rows.append(row)
    if not rows:
        errors.append('[http-inventory] company route inventory is empty')
    return rows, errors


def validate_capture(document: dict, capture: object, spec_hash: str, inventory: list) -> dict:
    errors = []
    if (not isinstance(capture, dict) or type(capture.get('version')) is not int or capture.get('version') != 1
            or capture.get('producer') != 'TestCompanyHTTPContract'
            or capture.get('spec_sha256') != spec_hash
            or not isinstance(capture.get('cases'), list) or not 1 <= len(capture['cases']) <= 1000):
        return {'status': 'fail', 'errors': ['[http-capture] missing, stale, or invalid capture'], 'responses': 0}
    rows, inventory_errors = company_inventory(inventory)
    if inventory_errors:
        return {'status': 'fail', 'errors': inventory_errors, 'responses': len(capture['cases'])}
    expected_operations = {(r['method'], r['path']) for r in rows}
    validator = HTTPContractValidator(document)
    ids, success = set(), set()
    for case in capture['cases']:
        case_errors = validator.validate_response(case)
        errors += case_errors
        if not isinstance(case, dict) or not isinstance(case.get('id'), str):
            continue
        if case['id'] in ids:
            errors.append('[http-capture] duplicate case id')
        ids.add(case['id'])
        expected_case = REQUIRED_CASES.get(case['id'])
        observed_case = (case.get('method'), case.get('route'), case.get('status'))
        if expected_case is not None and observed_case != expected_case:
            errors.append(f'[http-case-mismatch] {case["id"]}: expected operation/status not observed')
        if not case_errors and (case.get('method'), case.get('route')) not in expected_operations:
            errors.append(f'[http-inventory] {case["id"]}: response operation is absent from inventory')
        if not case_errors and type(case.get('status')) is int and 200 <= case['status'] < 300:
            success.add((case.get('method'), case.get('route'), str(case['status'])))
    for name in sorted(set(REQUIRED_CASES) - ids):
        errors.append(f'[http-missing-case] {name}')
    required = set()
    excluded = []
    for row in rows:
        if row['handler'] in LIVE_DNS_EXCLUSIONS:
            excluded.append({'method': row['method'], 'path': row['path'], 'reason': LIVE_DNS_EXCLUSIONS[row['handler']]})
            continue
        bindings = [contracts.COMPANY_RESPONSES[row['handler']]] if row['handler'] in contracts.COMPANY_RESPONSES else [('200', '', '')]
        if row['handler'] in contracts.COMPANY_ALTERNATE_RESPONSES:
            bindings.append(contracts.COMPANY_ALTERNATE_RESPONSES[row['handler']])
        for status, _, _ in bindings:
            required.add((row['method'], row['path'], status))
    for key in sorted(required - success):
        errors.append(f'[http-missing-success] {key}')
    return {'status': 'fail' if errors else 'pass', 'errors': errors,
            'responses': len(capture['cases']), 'successful_operations': len({x[:2] for x in success}),
            'required_success_variants': len(required), 'excluded_operations': excluded,
            'spec_sha256': spec_hash}


def run_process_group(command, *, cwd, env, stdout, stderr, timeout):
    """Bound the complete Go compiler/test process group, not only its leader."""
    proc = subprocess.Popen(command, cwd=cwd, env=env, stdout=stdout, stderr=stderr,
                            start_new_session=True)
    try:
        return subprocess.CompletedProcess(command, proc.wait(timeout=timeout))
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pass
        finally:
            # A child can ignore TERM even after the group leader has exited.
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.wait(timeout=5)
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--source-sha', required=True)
    args = parser.parse_args()
    if not re.fullmatch('[0-9a-f]{40}', args.source_sha):
        parser.error('source-sha must identify the frozen commit or tree (40 hex characters)')
    output = args.output_dir.resolve()
    # Captures include synthetic invitation secrets. Restrict the evidence
    # directory independently of the caller's umask, including failed runs.
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    report = {'status': 'fail', 'source_sha': args.source_sha}
    try:
        spec_bytes = contracts.OPENAPI_SPEC.read_bytes()
        document, errors = contracts.parse_openapi_text(spec_bytes.decode('utf-8'))
        if errors:
            raise ValueError('; '.join(errors))
        HTTPContractValidator(document)  # fail on dependencies/schema before running Go
        inventory = strict_json((ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
        static_errors = contracts.check_company_operations(document, inventory)
        if static_errors:
            raise ValueError('static operation bindings failed: ' + '; '.join(static_errors))
        env = dict(os.environ)
        env['TABMAIL_HTTP_CONTRACT_CAPTURE'] = str(output / 'responses.json')
        command = ['go', 'test', '-mod=readonly', '-json', '-race', '-count=1', '-timeout=180s', './internal/api', '-run', '^TestCompanyHTTPContract$']
        (output / 'command.json').write_text(json.dumps(command) + '\n')
        with (output / 'go-test.jsonl').open('w') as out, (output / 'go-test.stderr').open('w') as err:
            result = run_process_group(command, cwd=ROOT, env=env, stdout=out, stderr=err, timeout=240)
        (output / 'go-test.exit').write_text(str(result.returncode) + '\n')
        gate_cmd = [sys.executable, '-B', str(ROOT / 'scripts/check_go_test_evidence.py'), '--suite', 'http-contract',
                    '--log', str(output / 'go-test.jsonl'), '--exit-code', str(result.returncode), '--source-sha', args.source_sha]
        with (output / 'go-evidence.json').open('w') as out:
            evidence = subprocess.run(gate_cmd, cwd=ROOT, stdout=out, stderr=subprocess.PIPE, text=True, timeout=30)
        if evidence.returncode:
            raise ValueError('Go execution evidence failed; see go-test.jsonl and go-evidence.json')
        with (output / 'responses.json').open('rb') as stream:
            raw = stream.read(MAX_CAPTURE_BYTES + 1)
        if len(raw) > MAX_CAPTURE_BYTES:
            raise ValueError('capture exceeds bounded input size')
        capture = strict_json(raw.decode('utf-8'))
        report.update(validate_capture(document, capture, digest(spec_bytes), inventory))

        report['capture_sha256'] = digest(raw)
    except Exception as exc:
        report.update(status='fail', errors=[f'{type(exc).__name__}: {exc}'])
    (output / 'result.json').write_text(json.dumps(report, indent=2, ensure_ascii=False) + '\n')
    print(json.dumps(report, indent=2, ensure_ascii=False))
    return 0 if report['status'] == 'pass' else 1


if __name__ == '__main__':
    sys.exit(main())
