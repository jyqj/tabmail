from contextlib import nullcontext
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import backend_single as runner


class SingleProcessTests(unittest.TestCase):
    def test_whole_repository_command_is_unchanged(self):
        self.assertEqual(runner.GO_ARGV, ['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', './...'])

    def test_original_checker_and_exit_status_reject_every_false_green(self):
        for mode in ('pass', 'failed', 'skip', 'missing', 'nonzero', 'timeout'):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                output = Path(tmp) / 'evidence'
                calls = []
                def execute(argv, prefix, **kwargs):
                    calls.append(argv)
                    events = [dict(Package='p', Action='start')]
                    if mode != 'missing':
                        events += [dict(Package='p', Test='TestRequired', Action='run'),
                                   dict(Package='p', Test='TestRequired', Action='fail' if mode == 'failed' else 'skip' if mode == 'skip' else 'pass')]
                    events += [dict(Package='p', Action='fail' if mode == 'failed' else 'pass')]
                    prefix.with_suffix('.stdout').write_text('\n'.join(map(json.dumps, events)) + '\n')
                    return dict(argv=argv, exit_code=1 if mode in ('failed', 'nonzero') else 0, timeout=mode == 'timeout')
                env = dict(TABMAIL_TEST_DB_DSN='postgres://owned@127.0.0.1:55447/test',
                           TABMAIL_PAIR_REVIEW='1', TABMAIL_SUCCESSOR_AUTHZ_OWNED='1', TABMAIL_LF01_OWNED_PROBE='1')
                with mock.patch.dict(os.environ, env), mock.patch.object(runner, 'source_identity', return_value='a'*40), \
                     mock.patch.object(runner, 'owned_ui_dependencies', return_value=nullcontext()), \
                     mock.patch.object(runner, 'load_suite', return_value=({('p', 'TestRequired')}, {})), \
                     mock.patch.object(runner, 'run', side_effect=execute), mock.patch('builtins.print'):
                    result = runner.main(['--source-sha', 'a'*40, '--output-dir', str(output)])
                self.assertEqual(result, 0 if mode == 'pass' else 1)
                self.assertEqual(calls, [runner.GO_ARGV])
                self.assertEqual((output / 'go-test.jsonl').read_bytes(), (output / 'go-test.stdout').read_bytes())

    def test_real_process_timeout_is_joined_and_recorded(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = runner.run([sys.executable, '-c', 'import time; time.sleep(60)'],
                                Path(tmp) / 'slow', timeout=0.05)
            self.assertTrue(result['timeout'])
            self.assertNotEqual(result['exit_code'], 0)
            self.assertLess(result['elapsed_seconds'], 3)


if __name__ == '__main__':
    unittest.main()
