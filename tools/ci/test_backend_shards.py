import json
import hashlib
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
from backend_shards import discover, partition, validate, matrix_indices, test_argv, verify_matrix


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

    def test_only_an_original_explicit_skip_rule_can_allow_a_skip(self):
        events = log(dict(Action='start'), dict(Action='run', Test='TestA'),
                     dict(Action='skip', Test='TestA'), dict(Action='pass'))
        self.assertEqual(validate(events, {('p', 'TestA')}, {('p', 'TestA'): 'existing rule'}, 0)['status'], 'pass')

    def test_matrix_partition_is_exact_and_invalid_index_rejected(self):
        indices = [index for partition in range(4) for index in matrix_indices(partition)]
        self.assertEqual(sorted(indices), list(range(16)))
        for index in (-1, 4):
            with self.assertRaises(ValueError):
                matrix_indices(index)
        self.assertEqual(test_argv({('p', 'TestA')}),
                         ['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', '-run', '^(TestA)$', 'p'])

    def test_matrix_revalidates_all_original_logs_and_rejects_tampering(self):
        for mode in ('valid', 'missing', 'source', 'command', 'log', 'failure', 'extra', 'mandatory'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                artifacts, output = root / 'artifacts', root / 'result'
                artifacts.mkdir(); output.mkdir()
                source = 'a' * 40
                args = SimpleNamespace(verify_matrix=artifacts, source_sha=source)
                parts = [{('p', f'Test{i}')} for i in range(4)]
                plan = {'source_sha': source}
                for index, part in enumerate(parts):
                    folder = artifacts / f'company-backend-shards-{index}'
                    folder.mkdir()
                    name = f'Test{index}'
                    raw = '\n'.join(log(dict(Action='start'), dict(Action='run', Test=name),
                                         dict(Action='pass', Test=name), dict(Action='pass'))).encode()
                    for filename, data in {
                        'plan.json': plan,
                        'summary.json': dict(source_sha=source, matrix_index=index, shard_indices=[index],
                                             status='pass', source_unchanged=True),
                        f'shard-{index:02d}.process.json': dict(argv=test_argv(part), exit_code=0, timeout=False),
                        f'shard-{index:02d}.evidence.json': dict(source_sha=source, log_sha256=hashlib.sha256(raw).hexdigest()),
                    }.items():
                        (folder / filename).write_text(json.dumps(data))
                    (folder / f'shard-{index:02d}.jsonl').write_bytes(raw)
                folder = artifacts / 'company-backend-shards-0'
                if mode == 'missing':
                    (folder / 'shard-00.jsonl').unlink()
                elif mode == 'source':
                    (folder / 'plan.json').write_text(json.dumps({'source_sha': 'b' * 40}))
                elif mode == 'command':
                    (folder / 'shard-00.process.json').write_text(json.dumps(dict(argv=['true'], exit_code=0, timeout=False)))
                elif mode == 'log':
                    (folder / 'shard-00.jsonl').write_text('')
                elif mode == 'failure':
                    (folder / 'summary.json').write_text(json.dumps(dict(source_sha=source, matrix_index=0,
                        shard_indices=[0], status='fail', source_unchanged=True)))
                elif mode == 'extra':
                    (folder / 'shard-99.jsonl').write_text('')
                required = {('p', 'TestMissing')} if mode == 'mandatory' else {('p', 'Test0')}
                with mock.patch('backend_shards.subprocess.check_output', side_effect=[b'', source + '\n']):
                    if mode == 'valid':
                        self.assertEqual(verify_matrix(args, output, plan, parts, {}, required), 0)
                    else:
                        with self.assertRaises(ValueError):
                            verify_matrix(args, output, plan, parts, {}, required)


if __name__ == '__main__':
    unittest.main()
