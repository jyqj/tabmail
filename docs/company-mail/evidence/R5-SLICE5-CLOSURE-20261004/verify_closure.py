"""Bounded read-only source authentication and guarded synthetic pure validators.
No selected metadata producer, Go/Node, runtime, services or discovery.
"""
import ast, copy, hashlib, importlib, io, json, os, shutil, subprocess, sys, tempfile, unittest
from pathlib import Path
from unittest import mock
ROOT = Path(__file__).resolve().parents[4]
OUT = Path(__file__).resolve().parent
DELIVERY = '593bbc6a3ac91b5dbf8917602e45900bff094e27'
IMPLEMENTATION = '73777de3f51ff185d02ab2e1193421fa91cde9d4'
def git(*args):
    return subprocess.check_output(['git','-C',str(ROOT),*args])
def sha(raw): return hashlib.sha256(raw).hexdigest()
assert git('rev-parse','HEAD').decode().strip() == DELIVERY
records = {}
for row in git('ls-tree','-rz',DELIVERY).split(b'\0'):
    if not row: continue
    meta, name = row.split(b'\t'); mode, kind, blob = meta.decode().split(); name = name.decode()
    raw = (ROOT/name).read_bytes()
    assert hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest() == blob, name
    records[name] = dict(mode=mode, git_blob=blob, sha256=sha(raw), bytes=len(raw))
assert all(p.startswith('docs/company-mail/') for p in git('diff','--name-only',IMPLEMENTATION,DELIVERY).decode().splitlines())
inv = json.loads((ROOT/'docs/company-mail/evidence/R5-SLICE4-INTEGRATION-20261004/slice5-authority-inventory.json').read_bytes())
paths = inv['actual_changed_consumer_control_paths']; assert len(paths)==14
for p, r in {**paths, **inv['retained_authority_paths']}.items():
    assert records[p]['sha256']==r['sha256'] and records[p]['git_blob']==r['git_blob'], p
process_events=[]
def guard(event,args):
    if event in {'subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp'} or event.startswith('os.exec') or event.startswith('os.spawn'):
        process_events.append(event); raise RuntimeError('OS process forbidden: '+event)
sys.addaudithook(guard)
sys.path[:0]=[str(ROOT/'scripts'),str(ROOT/'scripts/tests')]
import r5_source_inventory as inventory
import r5_archive_boundary as boundary
import r5_selected_source_binding_v3 as v3
import test_r5_archive_boundary as archive_tests
import test_r5_smtp_owner_source_inventory as source_tests
import r5_selected_source_binding_v3_checks as selected_tests
names = inventory._current_source_paths(ROOT,'protocol',list(inventory.CURRENT_REPLACEMENTS))
assert set(paths)<=names
registry_reads=[]
for p in (ROOT/'scripts').rglob('*.py'):
    tree=ast.parse(p.read_text())
    for n in ast.walk(tree):
        if isinstance(n,ast.Constant) and n.value=='allowed_new_control_files':
            registry_reads.append(dict(path=str(p.relative_to(ROOT)),line=n.lineno,node=type(n).__name__))
assert registry_reads==[dict(path='scripts/r5_archive_boundary.py',line=155,node='Constant')], registry_reads
assert len(v3._V3_FILES)==3
# Synthetically owned frozen tracked files: no .git, no live metadata producer.
negative_rows=[]
class ChangedInputs(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory(prefix='r5-slice5-');cls.root=Path(cls.temp.name)
        for p in records:
            dest=cls.root/p;dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(ROOT/p,dest)
        cls.context=source_tests.context()
        cls.receipt=inventory.capture_current_source(cls.root,purpose='protocol',policy=inventory.ARCHIVE_POLICY,build_context=cls.context)
        assert set(paths)<=set(cls.receipt['files'])
        assert all(cls.receipt['files'][p]==records[p]['sha256'] for p in paths)
    @classmethod
    def tearDownClass(cls): cls.temp.cleanup()
    def test_each_changed_input_mutation_missing_extra_rename(self):
        for p in paths:
            target=self.root/p;raw=target.read_bytes();extra=target.with_name(target.name+'.slice5-extra')
            for mutation in ('bytes','missing','extra','rename'):
                with self.subTest(path=p,mutation=mutation):
                    if mutation=='bytes': target.write_bytes(raw+b'\n')
                    elif mutation=='missing': target.unlink()
                    elif mutation=='extra': extra.write_bytes(raw)
                    else: target.rename(extra)
                    try:
                        with self.assertRaises(ValueError) as caught:
                            inventory.validate_current_source(self.receipt,self.root,purpose='protocol',policy=inventory.ARCHIVE_POLICY)
                        negative_rows.append(dict(path=p,mutation=mutation,rejected=str(caught.exception)))
                    finally:
                        if extra.exists(): extra.unlink()
                        target.write_bytes(raw)
    def test_receipt_membership_and_versions(self):
        p=next(iter(paths))
        for mutation in ('omitted','extra','hash','schema_bool','schema2','schema3','policy','kind'):
            r=copy.deepcopy(self.receipt)
            if mutation=='omitted': del r['files'][p]
            elif mutation=='extra': r['files']['scripts/unseen.py']='a'*64
            elif mutation=='hash': r['files'][p]='a'*64
            elif mutation=='schema_bool': r['schema_version']=True
            elif mutation=='schema2': r['schema_version']=2
            elif mutation=='schema3': r['schema_version']=3
            elif mutation=='policy': r['policy']=inventory.CURRENT_POLICY
            else: r['source_identity_kind']='legacy'
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):
                inventory.validate_current_source(r,self.root,purpose='protocol',policy=inventory.ARCHIVE_POLICY)
            negative_rows.append(dict(domain='receipt',mutation=mutation,rejected=True))
    def test_v4_midflight_source_and_topology_drift(self):
        original=boundary.read;p=next(iter(paths));hits=0
        def drift(root,name):
            nonlocal hits
            raw=original(root,name)
            if name==p:
                hits+=1
                if hits>=3: return raw+b'\n'
            return raw
        with mock.patch.object(boundary,'read',side_effect=drift),self.assertRaises(ValueError):
            inventory.capture_current_source(self.root,purpose='protocol',policy=inventory.ARCHIVE_POLICY,build_context=self.context)
        real=boundary.check;hits=0
        def topology(root):
            nonlocal hits
            value=real(root);hits+=1
            return dict(value,synthetic_drift=True) if hits>1 else value
        with mock.patch.object(boundary,'check',side_effect=topology),self.assertRaisesRegex(ValueError,'changed during v4'):
            inventory.capture_current_source(self.root,purpose='protocol',policy=inventory.ARCHIVE_POLICY,build_context=self.context)
        negative_rows.extend([dict(domain='v4',mutation='midflight-bytes',rejected=True),dict(domain='v4',mutation='midflight-boundary',rejected=True)])
loader=unittest.TestLoader();suite=unittest.TestSuite()
suite.addTests(loader.loadTestsFromTestCase(ChangedInputs))
suite.addTests(loader.loadTestsFromTestCase(archive_tests.ArchiveBoundaryTests))
# Explicit pure methods only, not discovery or any formal runner.
for method in ('test_unknown_third_replace_and_extra_local_tree_fail_closed','test_wrong_root_versions_paths_and_dynamic_forms_rejected','test_local_module_identity_version_dependency_and_nested_replace_rejected','test_both_forks_root_modsum_and_metadata_drift_reject_pinned_receipt','test_symlink_escape_workfile_and_unknown_build_condition_fail_closed','test_unlisted_nested_module_and_local_import_omission_are_rejected','test_explicit_v3_pin_and_policy_are_required_without_auto_upgrade'):
    suite.addTest(source_tests.SMTPSourceInventoryV3Tests(method))
for method in ('test_external_pin_required_and_resigning_rejected','test_version_rejection_before_capture_no_fallback','test_validation_fresh_binding_mismatch_and_external_pin_precedes_execution'):
    suite.addTest(selected_tests.EnvelopeTests(method))
for method in ('test_v3_reuses_unchanged_v2_authority_predicates','test_v3_source_archive_package_coverage_and_replacement_rejections','test_source_before_after_drift_rejected'):
    suite.addTest((selected_tests.CaptureOrderingTests if method == "test_source_before_after_drift_rejected" else selected_tests.AuthorityTests)(method))
stream=io.StringIO();result=unittest.TextTestRunner(stream=stream,verbosity=2).run(suite)
# Authentication repeated after tests; no protected or tracked file may drift.
assert all(sha((ROOT/p).read_bytes())==r['sha256'] for p,r in records.items())
report=dict(delivery=DELIVERY,tested_implementation=IMPLEMENTATION,
    authenticated_frozen_files=len(records),input_paths={p:records[p] for p in paths},
    full_domain_membership={p:p in names for p in paths},registry_field_static_occurrences=registry_reads,
    v3_source_exact_keys=list(v3._V3_FILES),tests_run=result.testsRun,failures=len(result.failures),errors=len(result.errors),
    skips=len(result.skipped),os_process_events=process_events,executed_custom_negatives=negative_rows,
    preserved_tracked_files=records,scope='actual pure v4 validators on owned frozen copies; selected producer boundaries mocked; no physical qualification')
(OUT/'results.json').write_text(json.dumps(report,indent=2,sort_keys=True)+'\n')
(OUT/'checks.txt').write_text(stream.getvalue())
print(json.dumps({k:report[k] for k in ('authenticated_frozen_files','tests_run','failures','errors','skips','os_process_events')}))
if not result.wasSuccessful() or process_events: sys.exit(1)
