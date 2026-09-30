import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('r5_protocol', Path(__file__).resolve().parents[1] / 'check_r5_protocol.py')
protocol = importlib.util.module_from_spec(spec)
spec.loader.exec_module(protocol)


class CaseStructureTests(unittest.TestCase):
    def setUp(self):
        self.data = protocol.load_cases()

    def check_mutation(self, modify):
        data = copy.deepcopy(self.data)
        modify(data)
        with tempfile.TemporaryDirectory() as tmp:
            file = Path(tmp) / 'cases.json'
            file.write_text(json.dumps(data))
            with self.assertRaises(ValueError):
                protocol.load_cases(file)

    def test_full_case_set_does_not_claim_completion(self):
        report = protocol.summary(self.data)
        self.assertEqual(report['cases'], 46)
        self.assertEqual(report['shared_input_verified_cases'], 0)
        self.assertFalse(report['task_complete'])
        self.assertEqual(len(report['missing_shared_input_adapters']), 46)

    def test_missing_case_rejected(self):
        self.check_mutation(lambda d: d['cases'].pop())

    def test_duplicate_case_rejected(self):
        self.check_mutation(lambda d: d['cases'].append(d['cases'][0]))

    def test_empty_input_rejected(self):
        self.check_mutation(lambda d: d['cases'][0].update(input={}))

    def test_completion_claim_rejected(self):
        self.check_mutation(lambda d: d.update(task_complete=True))

    def test_invalid_status_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['expected'].update(http_status=999))

    def test_missing_literal_reason_must_keep_gap(self):
        self.check_mutation(lambda d: d['cases'][0].update(unresolved=[]))

    def test_reference_adapter_cannot_claim_shared_input(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(consumes_shared_input=True))

    def test_nonexistent_real_test_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(test='TestInventedProtocolPass'))

    def test_wrong_package_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(package='./internal/api'))

    def test_path_escape_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(source='../outside.go'))

    def test_audit_and_acceptance_coverage(self):
        self.assertEqual({a for row in self.data['cases'] for a in row['audit']}, {f'A{i:02d}' for i in range(1,8)})
        self.assertTrue(all(row['acceptance'] for row in self.data['cases']))


class ReferenceEvidenceTests(unittest.TestCase):
    package = 'tabmail/internal/store/postgres'
    name = 'TestRealProtocolReference'

    def events(self):
        return [{'Package':self.package,'Action':'start'},
                {'Package':self.package,'Action':'run','Test':self.name},
                {'Package':self.package,'Action':'pass','Test':self.name},
                {'Package':self.package,'Action':'pass'}]

    def classify(self, rows, code=0):
        return protocol.classify_events('\n'.join(map(json.dumps, rows)),self.package,{self.name},code)

    def test_named_pass_remains_reference_only(self):
        report = self.classify(self.events())
        self.assertFalse(report['errors'])
        self.assertFalse(report['task_complete'])
        self.assertEqual(report['status'], 'reference_tests_passed')

    def test_skip_rejected(self):
        events = self.events(); events[2]['Action'] = 'skip'
        self.assertTrue(self.classify(events)['errors'])

    def test_baseline_failure_is_not_protocol_pass(self):
        events = self.events(); events[2]['Action'] = 'fail'; events[-1]['Action'] = 'fail'
        self.assertTrue(self.classify(events, 1)['errors'])

    def test_missing_test_rejected(self):
        self.assertTrue(self.classify([self.events()[-1]])['errors'])

    def test_duplicate_terminal_rejected(self):
        events = self.events(); events.insert(3, events[2].copy())
        self.assertTrue(self.classify(events)['errors'])

    def test_compile_failure_rejected(self):
        self.assertTrue(self.classify([{'Package':self.package,'Action':'fail'}],1)['errors'])

    def test_cached_pass_without_run_rejected(self):
        events = self.events(); del events[1]
        self.assertTrue(self.classify(events)['errors'])

    def test_subtest_failure_rejected(self):
        events = self.events(); events.insert(2, {'Package':self.package,'Action':'fail','Test':self.name+'/expired'})
        self.assertTrue(self.classify(events)['errors'])

    def test_unexpected_package_rejected(self):
        events = self.events(); events[0]['Package'] = 'tabmail/fake'
        self.assertTrue(self.classify(events)['errors'])

    def test_unexpected_test_rejected(self):
        events = self.events(); events[1]['Test'] = 'TestOther'
        self.assertTrue(self.classify(events)['errors'])

    def test_malformed_json_rejected(self):
        with self.assertRaises(ValueError):
            protocol.classify_events('not json',self.package,{self.name},0)

class SharedConsumerTests(unittest.TestCase):
    def test_declared_shared_consumers_are_real_sources(self):
        data = protocol.load_cases()
        report = protocol.summary(data)
        self.assertEqual(report['cases_with_shared_input_adapters'], 4)
        # Structure remains zero runtime verification.
        self.assertEqual(report['shared_input_verified_cases'], 0)

    def test_component_pass_exact_case_set(self):
        report = protocol.classify_components({'success':True,'testResults':[{'assertionResults':[
            {'fullName':'R5 shared receipt RC03','status':'passed'}]}]}, {'R5 shared receipt RC03'}, 0)
        self.assertFalse(report['errors'])
        self.assertFalse(report['task_complete'])

    def test_component_missing_or_skipped_rejected(self):
        for status in ['skipped','pending','failed']:
            report = protocol.classify_components({'success':True,'testResults':[{'assertionResults':[
                {'fullName':'R5 shared receipt RC03','status':status}]}]}, {'R5 shared receipt RC03'}, 0)
            self.assertTrue(report['errors'])
        self.assertTrue(protocol.classify_components({'success':True,'testResults':[]},{'R5 shared receipt RC03'},0)['errors'])

    def test_component_duplicate_or_nonzero_exit_rejected(self):
        row={'fullName':'R5 shared receipt RC03','status':'passed'}
        self.assertTrue(protocol.classify_components({'success':True,'testResults':[{'assertionResults':[row,row]}]}, {row['fullName']}, 0)['errors'])
        self.assertTrue(protocol.classify_components({'success':True,'testResults':[{'assertionResults':[row]}]}, {row['fullName']}, 1)['errors'])

    def test_machine_markdown_output_drift_rejected(self):
        data = protocol.load_cases()
        data['cases'][0]['expected']['assertion'] = 'silently changed policy'
        with tempfile.TemporaryDirectory() as tmp:
            file = Path(tmp)/'cases.json';file.write_text(json.dumps(data))
            with self.assertRaises(ValueError): protocol.load_cases(file)


if __name__ == '__main__':
    unittest.main()
