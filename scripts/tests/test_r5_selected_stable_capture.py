"""Real cold/hot metadata roundtrips and fail-closed stable observation controls.

Private caches/disposable clones only; no product Go tests or service access.
"""
import copy
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_selected_source_binding as b

ROOT = Path(__file__).resolve().parents[2]
GO = Path('/workspace/tabmail-cloud/tools/go/bin/go')
HYDRATED = ['github.com/davecgh/go-spew@v1.1.1', 'github.com/go-test/deep@v1.1.1',
            'github.com/pmezard/go-difflib@v1.0.0', 'github.com/stretchr/testify@v1.11.1',
            'gopkg.in/yaml.v3@v3.0.1']


def identity(receipt):
    value = copy.deepcopy(receipt)
    value.pop('attestation_sha256')
    for command in value['commands']:
        command.pop('stderr')
    return value


def resign(receipt):
    receipt['attestation_sha256'] = hashlib.sha256(b.inventory.canonical(
        {k: v for k, v in receipt.items() if k != 'attestation_sha256'})).hexdigest()


class StableCaptureTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='selected-stable-')
        cls.addClassCleanup(cls.temp.cleanup)
        cls.state = Path(cls.temp.name)
        cls.root = cls.state/'checkout'
        subprocess.run(['git', 'clone', '--quiet', '--local', '--no-hardlinks', str(ROOT), str(cls.root)], check=True)
        for name in ['scripts/r5_selected_source_binding.py', 'scripts/tests/test_r5_selected_source_binding.py',
                     'scripts/tests/test_r5_selected_stable_capture.py']:
            shutil.copyfile(ROOT/name, cls.root/name)
        cls.mod = cls.state/'gomod'
        shutil.copytree('/workspace/tabmail-cloud/gomod', cls.mod)
        # Recreate PR20's incomplete cache even if the shared seed is now warm.
        # Keep authenticated download cache entries; remove extracted test deps.
        for name in HYDRATED:
            target = cls.mod/name
            if target.exists():
                for directory, _, _ in os.walk(target):
                    os.chmod(directory, 0o755)
                shutil.rmtree(target)
        cls.kw = dict(cache=cls.state/'gocache', modulecache=cls.mod)

    def roundtrip(self):
        receipt = b.capture(self.root, GO, **self.kw)
        observed = b.validate(receipt, self.root, GO, **self.kw)
        self.assertEqual(identity(receipt), identity(observed))
        return receipt

    def test_01_cold_capture_immediate_validate(self):
        self.assertTrue(all(not (self.mod/name).exists() for name in HYDRATED))
        self.cold = self.roundtrip()
        self.assertTrue(all((self.mod/name).is_dir() for name in HYDRATED))
        r = self.cold
        self.assertEqual(len(r['root_mvs']), 138)
        self.assertEqual(r['package_records'], 630)
        self.assertEqual(len(r['selected_local']), 613)
        self.assertEqual(len(r['generated_testmain']), 60)
        self.assertEqual(len(b.STATIC_EVIDENCE), 21)
        self.assertEqual([c['argv'][1:] for c in r['commands']],
                         [['env', '-json'], b.ARGV, b.MVS_ARGV, b.ARGV, b.MVS_ARGV])
        self.assertEqual(r['commands'][1]['stdout_sha256'], r['commands'][3]['stdout_sha256'])
        self.assertEqual(r['commands'][2]['stdout_sha256'], r['commands'][4]['stdout_sha256'])
        output = os.environ.get('R5_SELECTED_EVIDENCE_DIR')
        if output:
            destination = Path(output); destination.mkdir(parents=True, exist_ok=True)
            with gzip.open(destination/'cold-attestation.json.gz', 'wb') as f:
                f.write(b.inventory.canonical(r))
        print('COLD_PASS MVS=138 packages=630 local=613 generated=60 historical=21', flush=True)

    def test_02_hot_capture_immediate_validate(self):
        self.assertTrue(all((self.mod/name).is_dir() for name in HYDRATED))
        self.roundtrip()
        print('HOT_PASS', flush=True)

    def test_raw_mvs_drift_even_when_normalized_equal_rejected(self):
        actual = subprocess.run; count = 0
        def drift(argv, **kwargs):
            nonlocal count
            result = actual(argv, **kwargs)
            if argv[1:] == b.MVS_ARGV:
                count += 1
                if count == 2:
                    return subprocess.CompletedProcess(argv, 0, result.stdout+b'\n', result.stderr)
            return result
        with mock.patch.object(b.subprocess, 'run', side_effect=drift):
            with self.assertRaisesRegex(ValueError, 'metadata changed'):
                b.capture(self.root, GO, **self.kw)
        self.assertEqual(count, 2)

    def test_resigned_raw_hash_forgery_rejected_by_live_recapture(self):
        receipt = b.capture(self.root, GO, **self.kw)
        receipt['commands'][2]['stdout_sha256'] = '0'*64
        resign(receipt)
        with self.assertRaisesRegex(ValueError, 'mismatch'):
            b.validate(receipt, self.root, GO, **self.kw)

    def test_old_selected_v1_refused_before_dispatch(self):
        legacy = dict(policy='r5_root_selected_local_attestation_v1', schema_version=1)
        with mock.patch.object(b, 'capture', side_effect=AssertionError('must reject before dispatch')):
            with self.assertRaisesRegex(ValueError, 'incompatible'):
                b.validate(legacy, self.root, GO, **self.kw)

    def disposable_clone(self):
        directory = tempfile.TemporaryDirectory(dir=self.state)
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)/'checkout'
        subprocess.run(['git', 'clone', '--quiet', '--local', '--no-hardlinks', str(ROOT), str(root)], check=True)
        for name in ['scripts/r5_selected_source_binding.py', 'scripts/tests/test_r5_selected_source_binding.py',
                     'scripts/tests/test_r5_selected_stable_capture.py']:
            shutil.copyfile(ROOT/name, root/name)
        return root

    def test_actual_selected_file_change_rejected(self):
        root = self.disposable_clone()
        receipt = b.capture(root, GO, **self.kw)
        path = root/'internal/mailcontent/parser.go'
        self.assertIn(path.relative_to(root).as_posix(), receipt['selected_local'])
        path.write_bytes(path.read_bytes()+b'\n// stable capture source mutation control\n')
        with self.assertRaisesRegex(ValueError, 'mismatch') as refused:
            b.validate(receipt, root, GO, **self.kw)
        print('ACTUAL_SELECTED_CHANGE_REJECT', str(refused.exception), flush=True)

    def test_actual_source_change_during_initial_selection_rejected(self):
        root = self.disposable_clone()
        actual = subprocess.run; changed = False
        def mutate(argv, **kwargs):
            nonlocal changed
            result = actual(argv, **kwargs)
            if argv[1:] == b.ARGV and not changed:
                path = root/'internal/mailcontent/parser.go'
                path.write_bytes(path.read_bytes()+b'\n// mutation during first selection\n')
                changed = True
            return result
        with mock.patch.object(b.subprocess, 'run', side_effect=mutate):
            with self.assertRaisesRegex(ValueError, 'changed'):
                b.capture(root, GO, **self.kw)
        self.assertTrue(changed)

    def test_actual_module_change_rejected(self):
        root = self.disposable_clone()
        receipt = b.capture(root, GO, **self.kw)
        path = root/'go.mod'
        original = path.read_text()
        self.assertIn('golang.org/x/text v0.40.0', original)
        path.write_text(original.replace('golang.org/x/text v0.40.0', 'golang.org/x/text v0.39.0'))
        with self.assertRaises(ValueError) as refused:
            b.validate(receipt, root, GO, **self.kw)
        print('ACTUAL_MODULE_CHANGE_REJECT', str(refused.exception), flush=True)


if __name__ == '__main__':
    unittest.main(verbosity=2)
