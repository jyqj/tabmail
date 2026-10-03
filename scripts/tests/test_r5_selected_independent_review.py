"""Independent bounded review of 529f547; real Go list positive control.

Injected command results below are negative fault tests derived from live output,
not positive metadata fixtures. No product Go tests or runtime are executed.
"""
import copy
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
STATE = Path('/workspace/selected-attestation-review-state')
KW = dict(cache=STATE/'gocache', modulecache=STATE/'gomod')
CHECKPOINT = ROOT/'scripts/tests/checkpoints/r5_selected_independent_20261003'


def resign(value):
    value['attestation_sha256'] = hashlib.sha256(b.inventory.canonical(
        {k: v for k, v in value.items() if k != 'attestation_sha256'})).hexdigest()
    return value


class IndependentSelectedReview(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.raw = {}
        actual = subprocess.run
        def record(argv, **kwargs):
            result = actual(argv, **kwargs)
            cls.raw.setdefault(tuple(argv[1:]), result.stdout)
            return result
        with mock.patch.object(b.subprocess, 'run', side_effect=record):
            cls.receipt = b.capture(ROOT, GO, **KW)
        CHECKPOINT.mkdir(parents=True, exist_ok=True)
        (CHECKPOINT/'independent-attestation.log').write_bytes(b.inventory.canonical(cls.receipt))

    def test_live_original_contract_and_local_bytes(self):
        r = self.receipt
        self.assertEqual(r['commands'][2]['argv'][1:], [
            'list', '-mod=readonly', '-deps', '-test', '-race', '-tags=r5protocol',
            '-json', './...', 'github.com/jhillyerd/enmime/v2/...', 'github.com/emersion/go-smtp/...'])
        self.assertTrue(all(c['exit'] == 0 for c in r['commands']))
        self.assertNotIn('-buildvcs=false', str(r['commands']))
        self.assertEqual(r['go_env']['GOVERSION'], 'go1.25.7')
        self.assertEqual(len(b.STATIC_EVIDENCE), 21)
        self.assertTrue(b.STATIC_EVIDENCE.issubset(r['selected_local']))
        self.assertEqual({p: v['sha256'] for p, v in r['selected_local'].items()},
                         b.digest(ROOT, r['selected_local']))
        self.assertEqual(len(r['generated_testmain']), 60)
        self.assertTrue(r['external_modulecache_inputs'])
        self.assertTrue(r['toolchain_source_inputs'])
        self.assertTrue(r['native_inputs'])
        self.assertEqual(r['qualification']['overall'], 'blocked')
        for key in ['generated', 'external_modulecache', 'compiler_native', 'Method19', 'SML', 'wholeCI']:
            self.assertEqual(r['qualification'][key], 'unknown')
        embeds = {p for paths in r['base_source']['embed_inputs'].values() for p in paths}
        selected_embeds = {p for p, v in r['selected_local'].items() if set(v['fields']) & set(b.FIELDS[4:7])}
        self.assertTrue(selected_embeds)
        self.assertTrue(selected_embeds.issubset(embeds))
        modules = {m['Path']: m for m in r['root_mvs']}
        self.assertEqual(modules['golang.org/x/text']['Version'], 'v0.40.0')
        self.assertEqual([m['Replace'] for m in r['root_mvs'] if 'Replace' in m],
                         sorted(b.inventory.CURRENT_REPLACEMENTS, key=lambda m: m['module']))

    def test_live_roundtrip(self):
        self.assertEqual(b.validate(self.receipt, ROOT, GO, **KW), self.receipt)

    def test_first_capture_roundtrip_after_metadata_hydration(self):
        # Preserve the incomplete original cache as a seed; never write to it.
        # This regression intentionally remains red on the reviewed version.
        with tempfile.TemporaryDirectory(dir=STATE) as directory:
            modulecache = Path(directory)/'gomod'
            shutil.copytree('/workspace/tabmail-cloud/gomod', modulecache)
            kw = dict(cache=KW['cache'], modulecache=modulecache)
            first = b.capture(ROOT, GO, **kw)
            self.assertEqual(b.validate(first, ROOT, GO, **kw), first)

    def test_resigned_omission_extra_hash_environment_and_command_forgeries(self):
        # Each refusal recaptures actual Go metadata, including the context mutants.
        mutations = [
            ('missing-local', lambda r: r['selected_local'].pop(next(iter(r['selected_local'])))),
            ('extra-local', lambda r: r['selected_local'].update({'private.env': {'sha256':'a'*64,'fields':['GoFiles']}})),
            ('false-local-hash', lambda r: r['selected_local'][next(iter(r['selected_local']))].update(sha256='a'*64)),
            ('sample-metadata-hash', lambda r: r['commands'][2].update(stdout_sha256='b'*64)),
            ('wrong-root', lambda r: r['root_mvs'][next(i for i,m in enumerate(r['root_mvs']) if m.get('Main'))].update(Path='other')),
            ('wrong-ABI', lambda r: r['go_env'].update(GOAMD64='v3')),
            ('wrong-Go', lambda r: r['go_env'].update(GOVERSION='go1.25.6')),
            ('wrong-tags', lambda r: r['commands'][2]['argv'].__setitem__(6,'-tags=r5audit')),
            ('no-race', lambda r: r['commands'][2]['argv'].remove('-race')),
            ('wrong-CGO', lambda r: r['go_env'].update(CGO_ENABLED='0')),
            ('wrong-GOWORK', lambda r: r['environment'].update(GOWORK='/tmp/go.work')),
            ('wrong-flags', lambda r: r['environment'].update(GOFLAGS='-buildvcs=false')),
            ('generated-promoted', lambda r: r['generated_testmain'][0].update(qualification='complete')),
            ('closure-promoted', lambda r: r['qualification'].update(overall='complete')),
        ]
        for name, mutate in mutations:
            with self.subTest(name=name):
                forged = copy.deepcopy(self.receipt); mutate(forged); resign(forged)
                with self.assertRaises(ValueError): b.validate(forged, ROOT, GO, **KW)

    def test_old_v3_receipt_refused_before_dispatch(self):
        with mock.patch.object(b, 'capture', side_effect=AssertionError('must refuse before dispatch')):
            with self.assertRaises(ValueError):
                b.validate(self.receipt['base_source'], ROOT, GO, **KW)

    def test_wrong_live_go_environment_refused(self):
        actual = subprocess.run
        for key, value in [('GOVERSION','go1.25.6'), ('GOAMD64','v3'), ('CGO_ENABLED','0'),
                           ('GOWORK','/tmp/go.work'), ('GOFLAGS','-buildvcs=false'), ('GOEXPERIMENT','arenas')]:
            def fault(argv, **kwargs):
                result = actual(argv, **kwargs)
                if argv[1:] == ['env','-json']:
                    env = json.loads(result.stdout); env[key] = value
                    return subprocess.CompletedProcess(argv, 0, json.dumps(env).encode(), b'')
                return result
            with self.subTest(key=key), mock.patch.object(b.subprocess, 'run', side_effect=fault):
                with self.assertRaisesRegex(ValueError, 'version/flags/ABI'): b.capture(ROOT, GO, **KW)

    def test_live_metadata_path_and_fork_faults_refused(self):
        rows = b.stream(self.raw[tuple(b.ARGV)])
        auth = set(self.receipt['base_source']['files']) | b.STATIC_EVIDENCE
        local = next(r for r in rows if r.get('Module',{}).get('Main') and r.get('GoFiles') and not r['ImportPath'].endswith('.test'))
        for path in ['../../etc/passwd.go', '/etc/passwd.go', str(ROOT/'extra_unclassified.go')]:
            row = copy.deepcopy(local); row['GoFiles'] = [path]
            with self.subTest(path=path), self.assertRaises(ValueError):
                b.classify([row], ROOT, auth, self.receipt['go_env'], self.receipt['root_mvs'])
        mvs = b.stream(self.raw[tuple(b.MVS_ARGV)])
        for mode in ['wrong-root','wrong-fork','third-replace']:
            changed = copy.deepcopy(mvs)
            if mode == 'wrong-root': next(m for m in changed if m.get('Main'))['Path'] = 'other'
            if mode == 'wrong-fork': next(m for m in changed if 'Replace' in m)['Version'] = 'v0.0.0'
            if mode == 'third-replace': next(m for m in changed if not m.get('Main') and 'Replace' not in m)['Replace'] = {'Path':'./third_party/other','Dir':str(ROOT/'third_party/other')}
            with self.subTest(mode=mode), self.assertRaises(ValueError): b.normalize_mvs(changed, ROOT)

    def test_selected_missing_links_nested_and_extra_inputs(self):
        # Small filesystem controls use real selected paths; no product file edits.
        chosen = next(iter(self.receipt['selected_local']))
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); path = root/chosen; path.parent.mkdir(parents=True)
            with self.assertRaises(ValueError): b.digest(root, {chosen})
            path.symlink_to(ROOT/chosen)
            with self.assertRaises(ValueError): b.digest(root, {chosen})
            with self.assertRaises(ValueError): b.topology(root)
            path.unlink(); path.write_bytes((ROOT/chosen).read_bytes())
            self.assertEqual(b.digest(root,{chosen})[chosen], self.receipt['selected_local'][chosen]['sha256'])
            nested = root/'internal/nested'; nested.mkdir(parents=True); (nested/'go.mod').write_text('module other\n')
            with self.assertRaisesRegex(ValueError,'nested'): b.topology(root)

    def test_actual_source_change_between_metadata_queries_refused(self):
        # A separate disposable checkout keeps the reviewed tree read-only.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)/'checkout'
            shutil.copytree(ROOT, root, ignore=shutil.ignore_patterns('__pycache__'))
            actual = subprocess.run; changed = False
            def change(argv, **kwargs):
                nonlocal changed
                result = actual(argv, **kwargs)
                if argv[1:] == b.ARGV and not changed:
                    p = root/'internal/mailcontent/parser.go'
                    p.write_bytes(p.read_bytes()+b'\n// independent mid-capture fault\n')
                    changed = True
                return result
            with mock.patch.object(b.subprocess,'run',side_effect=change):
                with self.assertRaisesRegex(ValueError,'changed'): b.capture(root, GO, **KW)
            self.assertTrue(changed)


if __name__ == '__main__': unittest.main(verbosity=2)
