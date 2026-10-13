import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from backend_shards import discover, partition, validate


def log(*events):
    return [json.dumps(dict(Package='p', **event)) for event in events]


class CoverageTests(unittest.TestCase):
    def test_default_discovery_includes_fuzz_and_examples_not_benchmarks(self):
        events = log(dict(Action='start'), dict(Action='output', Output='TestA\n'),
                     dict(Action='output', Output='FuzzA\n'), dict(Action='output', Output='ExampleFoo_bar\n'),
                     dict(Action='output', Output='BenchmarkA\n'), dict(Action='pass'))
        self.assertEqual(discover(events, {'p'}), {('p', 'TestA'), ('p', 'FuzzA'), ('p', 'ExampleFoo_bar')})

    def test_discovery_rejects_incomplete_missing_duplicate_and_fail(self):
        good = log(dict(Action='start'), dict(Action='output', Output='TestA\n'), dict(Action='pass'))
        for events, packages in [
            (good[:-1], {'p'}), (good, {'p', 'missing'}), (good + good, {'p'}),
            (good[:2] + good[1:], {'p'}),
            (good[:2] + log(dict(Action='fail')), {'p'}),
            (good[:2] + log(dict(Action='skip')), {'p'}),
            (log(dict(Action='start'), dict(Action='pass')), {'p'}),
            (good, {'another'}),
        ]:
            with self.subTest(events=events, packages=packages), self.assertRaises(ValueError):
                discover(events, packages)

    def test_partition_is_exact_and_same_name_is_never_repeated_across_shards(self):
        tests = {('p', f'Test{i}') for i in range(37)} | {('q', 'Test3')}
        parts = partition(tests)
        flat = [test for part in parts for test in part]
        self.assertEqual(set(flat), tests)
        self.assertEqual(len(flat), len(tests))
        self.assertEqual(sum(any(name == 'Test3' for _, name in part) for part in parts), 1)
        self.assertTrue(all(parts))

    def test_empty_partition_rejected(self):
        with self.assertRaises(ValueError):
            partition(set())

    def test_exact_execution_passes_original_checker(self):
        events = log(dict(Action='start'), dict(Action='run', Test='TestA'),
                     dict(Action='pass', Test='TestA'), dict(Action='pass'))
        self.assertEqual(validate(events, {('p', 'TestA')}, {}, 0)['status'], 'pass')

    def test_skip_fail_missing_duplicate_and_nonzero_cannot_pass(self):
        good = log(dict(Action='start'), dict(Action='run', Test='TestA'),
                   dict(Action='pass', Test='TestA'), dict(Action='pass'))
        cases = [
            (good, {('p', 'TestA')}, 1), (good, {('p', 'TestMissing')}, 0),
            (good, {('p', 'TestA'), ('p', 'TestMissing')}, 0),
            (good[:-1], {('p', 'TestA')}, 0), (good + good, {('p', 'TestA')}, 0),
            (good[:2] + log(dict(Action='skip', Test='TestA'), dict(Action='pass')), {('p', 'TestA')}, 0),
            (good[:2] + log(dict(Action='fail', Test='TestA'), dict(Action='fail')), {('p', 'TestA')}, 0),
        ]
        for events, planned, code in cases:
            with self.subTest(events=events, planned=planned, code=code):
                self.assertEqual(validate(events, planned, {}, code)['status'], 'fail')

    def test_skipped_subtest_cannot_be_hidden_by_passing_parent(self):
        events = log(dict(Action='start'), dict(Action='run', Test='TestA'),
                     dict(Action='run', Test='TestA/child'), dict(Action='skip', Test='TestA/child'),
                     dict(Action='pass', Test='TestA'), dict(Action='pass'))
        self.assertEqual(validate(events, {('p', 'TestA')}, {}, 0)['status'], 'fail')


if __name__ == '__main__':
    unittest.main()
