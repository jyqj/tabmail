"""Validation-tool negatives only; synthetic packets here are never UI evidence."""
import copy
import re
import unittest

ALLOWED_MARKERS = {
    'RC01': {'R5_PROTOCOL_UI_TARGET_RC01_BCC'},
    'RC02': {'R5_PROTOCOL_UI_TARGET_RC02_LEGACY_BYPASS'},
    'RC03': {'R5_PROTOCOL_UI_TARGET_RC03_BCC'},
    'LF01': {'R5_PROTOCOL_UI_TARGET_LF01_FROZEN'},
    'LF06': {'R5_PROTOCOL_UI_TARGET_LF06_LIFECYCLE'},
    'PE01': {'R5_PROTOCOL_UI_TARGET_PE01_OMITTED'},
    'PE02': {'R5_PROTOCOL_UI_TARGET_PE02_NULL', 'R5_PROTOCOL_UI_TARGET_PE02_OMITTED'},
    'PE03': {'R5_PROTOCOL_UI_TARGET_PE03_STALE'},
    'PE04': {'R5_PROTOCOL_UI_TARGET_PE04_ABA'},
}


def classify_packet(observations, report, exit_code, case_id, variant, cases_sha256):
    """Pure validation seam: this does not generate or substitute API responses."""
    errors = []
    if (observations.get('schema_version') != 1 or observations.get('case_id') != case_id
            or observations.get('variant') != variant or observations.get('case_sha256') != cases_sha256):
        errors.append('mismatched case/variant/source')
    trace = observations.get('trace', [])
    if not trace or any(not row.get('method') or not row.get('path', '').startswith('/api/')
                        or type(row.get('status')) is not int or not 200 <= row['status'] <= 599
                        or not re.fullmatch('[a-f0-9]{64}', row.get('response_sha256', '')) for row in trace):
        errors.append('missing real HTTP trace')
    if not isinstance(observations.get('state'), dict) or not observations['state']:
        errors.append('missing observed PostgreSQL state')
    def has_private(value):
        if isinstance(value, dict):
            return any(key.lower() in {'token', 'authorization', 'password', 'password_hash', 'headers', 'private_fixture'}
                       or has_private(child) for key, child in value.items())
        return isinstance(value, list) and any(has_private(child) for child in value)
    if has_private(observations):
        errors.append('private credentials cannot enter evidence archive')
    # Current Vitest JSON reporter omits this optional legacy counter. Its
    # absence is not a runtime error; an explicit malformed/nonzero value is.
    runtime_counter = report.get('numRuntimeErrorTestSuites', 0)
    if type(runtime_counter) is not int or runtime_counter != 0:
        errors.append('runtime error counter is malformed or nonzero')
    if any(report.get(key) for key in ['unhandledErrors', 'errors']) or report.get('numUnhandledErrors', 0) != 0:
        errors.append('unhandled runtime errors cannot be target red')
    required = ['numTotalTests', 'numFailedTests', 'numPendingTests']
    if any(type(report.get(key)) is not int or report[key] < 0 for key in required):
        errors.append('required execution counters missing or malformed')
    suites = report.get('testResults')
    if not isinstance(suites, list) or any(not isinstance(suite, dict) or not isinstance(suite.get('assertionResults'), list) for suite in suites):
        errors.append('malformed runtime suite/assertions')
        assertions = []
    else:
        assertions = [a for suite in suites for a in suite['assertionResults']]
    if any(not isinstance(a, dict) for a in assertions):
        errors.append('malformed runtime assertion')
        assertions = []
    expected = f'R5 protocol component {case_id} {variant} secure behavior'
    if (report.get('numTotalTests') != 1 or report.get('numPendingTests') != 0
            or len(assertions) != 1
            or assertions[0].get('fullName') != expected):
        errors.append('missing/duplicate/skipped/runtime/setup component execution')
    target = None
    if not errors:
        assertion = assertions[0]
        if assertion.get('status') == 'passed' and exit_code == 0 and report.get('numFailedTests') == 0:
            pass
        elif assertion.get('status') == 'failed' and exit_code == 1 and report.get('numFailedTests') == 1:
            messages = assertion.get('failureMessages', [])
            if not isinstance(messages,list) or any(not isinstance(message,str) for message in messages):
                errors.append('malformed failure messages')
                messages=[]
            joined = '\n'.join(messages)
            matches = re.findall(r'^Error: (R5_PROTOCOL_UI_TARGET_[A-Z0-9_]+): ', joined, re.M)
            if (len(messages) != 1 or len(matches) != 1 or matches[0] not in ALLOWED_MARKERS.get(case_id, set())
                    or any(value in joined for value in ['panic:', 'runtime error:', 'timed out', 'Unhandled Error'])):
                errors.append('non-target component failure')
            else:
                target = matches[0]
        else:
            errors.append('process/assertion/status contradiction')
    return {'status': 'invalid' if errors else 'target_red' if target else 'scoped_pass',
            'errors': errors, 'target_marker': target, 'process_exit_code': exit_code,
            'task_complete': False, 'product_green': False,
            'boundary': 'HTTP/PG source-linked component scope only, not shipping browser or all protocol completion'}


class ComponentPacketEvidenceTests(unittest.TestCase):
    def fixtures(self):
        observations = {'schema_version': 1, 'case_id': 'PE01', 'variant': 'default', 'case_sha256': 'a'*64,
                        'trace': [{'method': 'PUT', 'path': '/api/v1/admin/users/fixture/permissions', 'status': 200, 'response_sha256': 'b'*64}],
                        'state': {'effective_can_send': True}}
        report = {'numTotalTests': 1, 'numFailedTests': 1, 'numPendingTests': 0, 'numRuntimeErrorTestSuites': 0,
                  'testResults': [{'assertionResults': [{'fullName': 'R5 protocol component PE01 default secure behavior', 'status': 'failed',
                    'failureMessages': ['Error: R5_PROTOCOL_UI_TARGET_PE01_OMITTED: observed loss of an unedited restriction\n at actual source']}]}]}
        return observations, report

    def classify(self, observations, report, code=1):
        return classify_packet(observations, report, code, 'PE01', 'default', 'a'*64)

    def test_exact_source_linked_target_is_not_product_green(self):
        observations, report = self.fixtures(); result = self.classify(observations, report)
        self.assertEqual(result['status'], 'target_red')
        self.assertFalse(result['product_green']); self.assertFalse(result['task_complete'])
        self.assertEqual(result['process_exit_code'], 1)

    def test_hash_case_variant_mismatch_rejected(self):
        for key in ['case_id', 'variant', 'case_sha256']:
            observations, report = self.fixtures(); observations[key] = 'different'
            self.assertTrue(self.classify(observations, report)['errors'])

    def test_no_http_or_no_pg_observation_rejected(self):
        for key in ['trace', 'state']:
            observations, report = self.fixtures(); observations[key] = []
            self.assertTrue(self.classify(observations, report)['errors'])

    def test_credential_and_headers_rejected(self):
        for key in ['token', 'authorization', 'headers', 'password_hash']:
            observations, report = self.fixtures(); observations['nested'] = {key: 'not-a-real-secret'}
            self.assertTrue(self.classify(observations, report)['errors'])

    def test_compile_panic_timeout_setup_and_unknown_marker_rejected(self):
        for message in ['Compile failed', 'panic: callback', 'Test timed out after 30000ms',
                        'Error: R5_PROTOCOL_UI_TARGET_PE01_INVENTED: unexpected status',
                        'Error: unexpected fetch error\nsource snippet: R5_PROTOCOL_UI_TARGET_PE01_OMITTED:',
                        'Error: R5_PROTOCOL_UI_TARGET_PE01_OMITTED: intended\nUnhandled Error: fixture failed']:
            observations, report = self.fixtures(); report['testResults'][0]['assertionResults'][0]['failureMessages'] = [message]
            self.assertTrue(self.classify(observations, report)['errors'], message)

    def test_duplicate_or_skipped_or_runtime_suite_rejected(self):
        for change in ['duplicate', 'skip', 'runtime']:
            observations, report = self.fixtures()
            if change == 'duplicate': report['testResults'][0]['assertionResults'] *= 2
            elif change == 'skip': report['numPendingTests'] = 1
            else: report['numRuntimeErrorTestSuites'] = 1
            self.assertTrue(self.classify(observations, report)['errors'])

    def test_scoped_component_pass_never_closes_whole_product(self):
        observations, report = self.fixtures(); report['numFailedTests'] = 0
        assertion = report['testResults'][0]['assertionResults'][0]; assertion['status'] = 'passed'; assertion['failureMessages'] = []
        result = self.classify(observations, report, 0)
        self.assertEqual(result['status'], 'scoped_pass'); self.assertFalse(result['product_green'])

    def test_current_reporter_missing_optional_counter_is_valid(self):
        observations,report=self.fixtures()
        report.pop('numRuntimeErrorTestSuites')
        self.assertEqual(self.classify(observations,report)['status'],'target_red')
        report['numFailedTests']=0;assertion=report['testResults'][0]['assertionResults'][0]
        assertion['status']='passed';assertion['failureMessages']=[]
        self.assertEqual(self.classify(observations,report,0)['status'],'scoped_pass')

    def test_explicit_null_string_bool_or_nonzero_runtime_count_rejected(self):
        for value in [None,'0',False,1]:
            observations,report=self.fixtures();report['numRuntimeErrorTestSuites']=value
            self.assertTrue(self.classify(observations,report)['errors'])

    def test_unhandled_errors_stay_invalid_without_optional_counter(self):
        for key,value in [('unhandledErrors',['callback failed']),('errors',['runtime failed']),('numUnhandledErrors',1)]:
            observations,report=self.fixtures();report.pop('numRuntimeErrorTestSuites');report[key]=value
            self.assertTrue(self.classify(observations,report)['errors'])

    def test_missing_required_counter_unknown_test_wrong_exit_and_malformed_stay_invalid(self):
        for kind in ['required','unknown','exit','null_assertions','null_message']:
            observations,report=self.fixtures();report.pop('numRuntimeErrorTestSuites');code=1
            if kind=='required':report.pop('numTotalTests')
            elif kind=='unknown':report['testResults'][0]['assertionResults'][0]['fullName']='Unknown test'
            elif kind=='exit':code=124
            elif kind=='null_assertions':report['testResults'][0]['assertionResults']=None
            else:report['testResults'][0]['assertionResults'][0]['failureMessages']=[None]
            self.assertTrue(self.classify(observations,report,code)['errors'],kind)
