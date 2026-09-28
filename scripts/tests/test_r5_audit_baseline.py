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

if __name__=='__main__': unittest.main()
