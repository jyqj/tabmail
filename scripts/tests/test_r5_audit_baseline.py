import importlib.util
import json
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('r5_audit',Path(__file__).resolve().parents[1]/'run_r5_audit_baseline.py')
audit=importlib.util.module_from_spec(spec)
spec.loader.exec_module(audit)

class AuditEvidenceTests(unittest.TestCase):
    def events(self):
        p='tabmail/internal/store/postgres'
        rows=[{'Package':p,'Action':'start'}]
        for test,code in audit.EXPECTED.items():
            rows.extend([{'Package':p,'Test':test,'Action':'run'}, {'Package':p,'Test':test,'Action':'output','Output':f'R5_BASELINE_DEFECT_{code}: exact assertion'}, {'Package':p,'Test':test,'Action':'fail'}])
        rows.append({'Package':p,'Action':'fail'})
        return rows
    def result(self,rows,exit_code=1):return audit.classify('\n'.join(map(json.dumps,rows)),exit_code)
    def test_exact_failures_are_reproduction_not_fix(self):
        r=self.result(self.events());self.assertFalse(r['errors']);self.assertFalse(r['product_fixed']);self.assertEqual(len(r['reproduced']),7)
    def test_setup_failure_is_rejected(self):
        rows=self.events();rows[2]['Output']='database unavailable';self.assertTrue(self.result(rows)['errors'])
    def test_skip_is_rejected(self):
        rows=self.events();rows[3]['Action']='skip';self.assertTrue(self.result(rows)['errors'])
    def test_success_is_not_a_baseline_failure(self):self.assertTrue(self.result(self.events(),0)['errors'])
    def test_missing_test_is_rejected(self):self.assertTrue(self.result(self.events()[:-4])['errors'])
    def test_compile_failure_is_rejected(self):self.assertTrue(self.result([{'Package':'tabmail/internal/store/postgres','Action':'fail'}])['errors'])
    def test_duplicate_marker_is_rejected(self):
        rows=self.events();rows[2]['Output']*=2;self.assertTrue(self.result(rows)['errors'])
    def test_zero_tests_is_rejected(self):self.assertTrue(self.result([])['errors'])
    def test_invalid_json_is_not_reproduction(self):
        with self.assertRaises(ValueError):audit.classify('not json',1)

class ComponentEvidenceTests(unittest.TestCase):
    def events(self):
        package = 'tabmail/internal/store/postgres'
        events = [{'Package': package, 'Action': 'start'}]
        for name, code in audit.COMPONENT_EXPECTED.items():
            events.extend([{'Package':package,'Test':name,'Action':'run'},
                           {'Package':package,'Test':name,'Action':'output','Output':f'R5_COMPONENT_BASELINE_{code}: corroborated'},
                           {'Package':package,'Test':name,'Action':'fail'}])
        return events + [{'Package':package,'Action':'fail'}]
    def check(self, events): return audit.classify('\n'.join(map(json.dumps, events)),1,'components')
    def test_component_layer_is_exact_and_not_product_success(self):
        result=self.check(self.events());self.assertFalse(result['errors']);self.assertFalse(result['product_fixed']);self.assertEqual(result['reproduced'],['A01','A02'])
    def test_db_marker_cannot_substitute_for_component_execution(self):
        events=self.events();events[2]['Output']='R5_BASELINE_DEFECT_A01: HTTP only';self.assertTrue(self.check(events)['errors'])
    def test_component_setup_failure_rejected(self):
        events=self.events();events[2]['Output']='could not find dialog';self.assertTrue(self.check(events)['errors'])
    def test_component_missing_second_case_rejected(self): self.assertTrue(self.check(self.events()[:4]+self.events()[-1:])['errors'])
    def test_unknown_layer_rejected(self):
        with self.assertRaises(ValueError): audit.classify('',1,'browser')

if __name__=='__main__': unittest.main()
