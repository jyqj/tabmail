import sys
from pathlib import Path
import unittest
from unittest import mock
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import run_r5_source_version_tests as runner

class SourceVersionRunnerTests(unittest.TestCase):
    def setUp(self):
        self.ids=sorted(runner.HISTORICAL_IDS|{runner.FRESH_CLASS+'test_roundtrip','test_other.Class.test_x'})

    def test_exact_partition(self):
        current,frozen=runner.partition(self.ids)
        self.assertFalse(set(current)&set(frozen));self.assertEqual(set(current+frozen),set(self.ids))

    def test_missing_overlap_duplicate_wrong_baseline(self):
        current,frozen=runner.partition(self.ids)
        for a,b,c in [(current[1:],frozen,runner.BASELINE),(current+frozen[:1],frozen,runner.BASELINE),(current+current[:1],frozen,runner.BASELINE),(current,frozen,'a'*40)]:
            with self.subTest(a=a,b=b,c=c),self.assertRaises(ValueError):runner.verify(self.ids,a,b,c)

    def test_no_fresh_actual_root_class(self):
        with self.assertRaises(ValueError):runner.partition(list(runner.HISTORICAL_IDS))

    def test_child_failure_and_skip_not_green(self):
        import tempfile,json
        class Bad(unittest.TestCase):
            def test_fail(self):self.fail('intentional runner negative')
        class Skip(unittest.TestCase):
            @unittest.skip('intentional runner negative')
            def test_skip(self):pass
        for case in [Bad('test_fail'),Skip('test_skip')]:
            with tempfile.TemporaryDirectory() as temp,mock.patch.object(runner.unittest.TestLoader,'loadTestsFromNames',return_value=unittest.TestSuite([case])),mock.patch.object(runner,'git',return_value=runner.BASELINE),mock.patch.object(runner,'git_go_env',return_value='/tmp/r5-runner-cache'):
                before=Path.cwd()
                try:
                    self.assertEqual(runner.child(temp,[case.id()],Path(temp)/'report.json'),1)
                finally:
                    import os
                    os.chdir(before)

if __name__=='__main__':unittest.main()
