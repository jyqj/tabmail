"""Preserve evidence paths while retaining separately prepared protocol source.

Shell child doubles below test upload admission only. Their synthetic files are
never SOURCE, Go, PostgreSQL, or protocol execution evidence.
"""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
PROTOCOL_STEP = 'Real protocol baseline with explicit target failures'
SOURCE_UPLOAD = 'Upload protocol source manifest'
SOURCE_FILES = {'source-manifest.json', 'source-manifest.sha256', 'preparation.json'}
ORIGINAL_PATHS = [
    'source-version-tests.json', 'go-test.jsonl', 'go-test-source-sha.txt',
    'go-test-evidence.json', 'go-test-negative-evidence', 'http-contract-evidence',
    '!http-contract-evidence/responses.json', 'protocol-baseline-evidence',
    'backend-typescript-preparation.json',
]


def backend():
    workflow = yaml.load((ROOT / '.github/workflows/company-p0.yml').read_text(), Loader=yaml.BaseLoader)
    return workflow['jobs']['backend']


def step(name):
    return next(value for value in backend()['steps'] if value.get('name') == name)


class ProtocolArtifactLayoutTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='r5-protocol-artifact-control-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.checkout = self.root / 'checkout'
        self.checkout.mkdir()
        self.runner_temp = self.root / 'runner-temp'
        self.runner_temp.mkdir()
        self.step_output = self.root / 'github-output'
        self.calls = self.root / 'child-calls.jsonl'
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        child = self.bin / 'python3'
        child.write_text('#!' + sys.executable + '\n' + '''
import hashlib, json, os
from pathlib import Path
import sys
args = sys.argv[1:]
if args and args[0] == '-B': args = args[1:]
if args[0] == 'scripts/prepare_r5_protocol_source.py':
    kind = 'preparation'
    output = Path(args[args.index('--output-dir') + 1])
    output.mkdir(mode=0o700)
    raw = b'{"shell_control_only":true,"runtime_qualified":false}\\n'
    (output / 'source-manifest.json').write_bytes(raw)
    code = int(os.environ.get('CONTROL_PREPARATION_EXIT', '0'))
    if not code:
        (output / 'source-manifest.sha256').write_text(hashlib.sha256(raw).hexdigest() + '\\n')
        (output / 'preparation.json').write_text('{"status":"shell_control_only","product_green":false}\\n')
elif args[0] == 'scripts/check_r5_protocol.py':
    kind = 'protocol'
    code = int(os.environ.get('CONTROL_PROTOCOL_EXIT', '0'))
else:
    raise AssertionError('unreviewed shell child: ' + repr(args))
with open(os.environ['CONTROL_CALLS'], 'a') as stream:
    stream.write(json.dumps({'kind': kind, 'args': args}) + '\\n')
raise SystemExit(code)
''')
        child.chmod(0o755)

    def run_shell(self, **overrides):
        env = {**os.environ, 'PATH': str(self.bin) + os.pathsep + os.environ['PATH'],
               'RUNNER_TEMP': str(self.runner_temp), 'GITHUB_OUTPUT': str(self.step_output),
               'CONTROL_CALLS': str(self.calls), **overrides}
        result = subprocess.run(['bash', '-e', '-c', step(PROTOCOL_STEP)['run']], cwd=self.checkout,
                                env=env, capture_output=True, text=True, timeout=10)
        calls = [json.loads(line) for line in self.calls.read_text().splitlines()] if self.calls.exists() else []
        return result, calls

    def source_directory(self):
        self.assertTrue(self.step_output.is_file(), 'successful preparation must publish its source directory')
        outputs = dict(line.split('=', 1) for line in self.step_output.read_text().splitlines())
        self.assertEqual(set(outputs), {'source_directory'})
        return Path(outputs['source_directory'])

    def test_original_artifact_name_paths_exclusion_and_failure_upload_are_unchanged(self):
        upload = step('Upload test evidence')
        self.assertEqual(upload, {
            'name': 'Upload test evidence', 'if': 'always()', 'uses': 'actions/upload-artifact@v4',
            'with': {'name': 'company-p0-test-evidence', 'path': '\n'.join(ORIGINAL_PATHS) + '\n',
                     'if-no-files-found': 'error', 'retention-days': '14'},
        })

    def test_original_artifact_common_ancestor_stays_at_the_checkout(self):
        # upload-artifact documents that all positive search paths share one
        # least-common-ancestor root; exclusion paths do not change that root.
        checkout = PurePosixPath('/home/runner/work/tabmail/tabmail')
        runner_temp = '/home/runner/work/_temp'
        paths = [PurePosixPath(path.replace('${{ runner.temp }}', runner_temp))
                 for path in step('Upload test evidence')['with']['path'].splitlines()
                 if not path.startswith('!')]
        resolved = [str(path if path.is_absolute() else checkout / path) for path in paths]
        self.assertEqual(posixpath.commonpath(resolved), str(checkout))

    def test_source_has_one_separate_guarded_artifact_without_changing_legacy_root(self):
        matches = [value for value in backend()['steps'] if value.get('name') == SOURCE_UPLOAD]
        self.assertEqual(len(matches), 1)
        upload = matches[0]
        self.assertEqual(step(PROTOCOL_STEP)['id'], 'backend_protocol')
        self.assertEqual(upload, {
            'name': SOURCE_UPLOAD,
            'if': "${{ always() && steps.backend_protocol.outputs.source_directory != '' }}",
            'uses': 'actions/upload-artifact@v4',
            'with': {'name': 'company-p0-protocol-source-manifest',
                     'path': '${{ steps.backend_protocol.outputs.source_directory }}',
                     'if-no-files-found': 'error', 'retention-days': '14'},
        })
        self.assertNotIn('source_directory', step('Upload test evidence')['with']['path'])

    def test_failed_preparation_does_not_admit_partial_source_or_launch_protocol(self):
        result, calls = self.run_shell(CONTROL_PREPARATION_EXIT='17')
        self.assertEqual(result.returncode, 17, result.stderr)
        self.assertEqual([call['kind'] for call in calls], ['preparation'])
        # A failed helper can have written a partial receipt. Presence of that
        # file must not be substituted for the successful preparation output.
        self.assertTrue((self.runner_temp / 'tabmail-protocol-source/source-manifest.json').is_file())
        self.assertFalse(self.step_output.exists())

    def test_protocol_failure_stays_failed_and_retains_successfully_prepared_source(self):
        result, calls = self.run_shell(CONTROL_PROTOCOL_EXIT='23')
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assertEqual([call['kind'] for call in calls], ['preparation', 'protocol'])
        source = self.source_directory()
        self.assertEqual(source, self.runner_temp / 'tabmail-protocol-source')
        self.assertEqual({path.name for path in source.iterdir()}, SOURCE_FILES)
        self.assertNotIn('continue-on-error', step(PROTOCOL_STEP))

    def test_success_retains_all_three_prepared_files_outside_runtime_and_checkout(self):
        result, calls = self.run_shell()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([call['kind'] for call in calls], ['preparation', 'protocol'])
        source = self.source_directory()
        self.assertFalse(source.is_relative_to(self.checkout))
        self.assertFalse((self.checkout / 'protocol-baseline-evidence').exists())
        self.assertEqual({path.name for path in source.iterdir()}, SOURCE_FILES)
        raw = (source / 'source-manifest.json').read_bytes()
        self.assertEqual((source / 'source-manifest.sha256').read_text(), hashlib.sha256(raw).hexdigest() + '\n')


if __name__ == '__main__':
    unittest.main()
