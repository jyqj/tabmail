"""Explicitly invoked additive v3 checks; no formal routing/discovery migration."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import subprocess
import tempfile
from contextlib import ExitStack
import unittest
from unittest import mock
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_selected_source_binding_v3 as v3
import r5_selected_source_binding_v2 as v2


def wire(rows):
    return b'\n'.join(json.dumps(r, allow_nan=False).encode() for r in rows)


def sample(kind):
    if kind == 'string': return 'one'
    if kind == 'bool': return True
    if kind in ('[]string', '[]string|null'): return ['one', 'two', 'one']
    if kind == 'map[string]string': return {'Stale':'one', 'StaleReason':'two'}
    if kind == '*time.Time': return '2026-10-04T01:02:03.123456789Z'
    if kind == '[]*PackageError': return [sample('*PackageError')]
    nested = {'*modinfo.ModulePublic':'ModulePublic', '*ModulePublic':'ModulePublic',
              '*codehost.Origin':'Origin', '*PackageError':'PackageError', '*ModuleError':'ModuleError'}[kind]
    # Exercise recursive modules separately with bounded depth.
    return {k:sample(t) for k,t in v3.REGISTRY['types'][nested].items()
            if t != '*ModulePublic'}


def leaves(obj, prefix=()):
    if type(obj) is dict:
        for k,v in obj.items(): yield from leaves(v, prefix+(k,))
    elif type(obj) is list:
        for i,v in enumerate(obj): yield from leaves(v, prefix+(i,))
    else: yield prefix, obj


def change(obj, path, value):
    for key in path[:-1]: obj = obj[key]
    obj[path[-1]] = value


class ProjectionTests(unittest.TestCase):
    def setUp(self):
        self.row = {k:sample(t) for k,t in v3.REGISTRY['types']['PackagePublic'].items()}
        self.row['Module']['Replace'] = sample('*ModulePublic')
        self.row['Module']['Update'] = sample('*ModulePublic')
        self.row['Module']['Replace']['Replace'] = {'Path':'recursive'}

    def test_diagnostic_values_and_presence_equal_binding_different_observation(self):
        original = wire([self.row]); expected = v3.project_package_stdout(original)
        for stale, reason in [(False,''),(True,'other'),(None,None),(False,None),(None,'reason')]:
            row=copy.deepcopy(self.row)
            for key,value in [('Stale',stale),('StaleReason',reason)]:
                if value is None: row.pop(key)
                else: row[key]=value
            raw=wire([row])
            with self.subTest(stale=stale,reason=reason):
                self.assertEqual(expected,v3.project_package_stdout(raw))
                self.assertNotEqual(v3._sha(original),v3._sha(raw))

    def test_every_retained_field_and_nested_leaf_is_bound(self):
        expected=v3.project_package_stdout(wire([self.row]))
        for path,value in leaves(self.row):
            if path[0] in ('Stale','StaleReason'): continue
            row=copy.deepcopy(self.row)
            replacement = not value if type(value) is bool else ('2026-10-05T01:02:03Z' if path[-1]=='Time' else value+'changed')
            change(row,path,replacement)
            with self.subTest(path=path): self.assertNotEqual(expected,v3.project_package_stdout(wire([row])))
        for field in self.row:
            if field in ('Stale','StaleReason'): continue
            row=copy.deepcopy(self.row); row.pop(field)
            with self.subTest(absent=field): self.assertNotEqual(expected,v3.project_package_stdout(wire([row])))

    def test_recursive_module_field_presence_bound(self):
        original=wire([self.row]); expected=v3.project_package_stdout(original)
        for module in ('Replace','Update'):
            for field in v3.REGISTRY['types']['ModulePublic']:
                row=copy.deepcopy(self.row)
                row['Module'][module][field]=sample(v3.REGISTRY['types']['ModulePublic'][field])
                row['Module'][module].pop(field)
                if wire([row]) == original: continue
                with self.subTest(module=module,field=field): self.assertNotEqual(expected,v3.project_package_stdout(wire([row])))

    def test_order_arrays_multiplicity_and_presence_bound(self):
        a={'ImportPath':'one','Imports':['a','b','a']}; b={'ImportPath':'two'}
        original=v3.project_package_stdout(wire([a,b]))
        for rows in ([b,a],[a,a,b],[dict(a,Imports=['b','a','a']),b],[dict(a,Imports=['a','b']),b],[dict(a,ImportMap={}),b]):
            self.assertNotEqual(original,v3.project_package_stdout(wire(rows)))
        self.assertEqual(original,v3.project_package_stdout(json.dumps(a,sort_keys=True,indent=4).encode()+b' \n'+json.dumps(b).encode()))

    def test_unknown_fields_in_all_nested_domains_fail_closed(self):
        for path in [(),('Module',),('Module','Replace'),('Module','Update'),('Module','Origin'),('Error',),('DepsErrors',0),('Module','Error')]:
            for key in ('Unknown','Stale','StaleReason'):
                if not path and key in ('Stale','StaleReason'): continue
                row=copy.deepcopy(self.row); obj=row
                for part in path: obj=obj[part]
                obj[key]='unapproved'
                with self.subTest(path=path,key=key),self.assertRaises(ValueError): v3.project_package_stdout(wire([row]))
        # Same-named arbitrary ImportMap keys are legitimate strings and stay bound.
        row=copy.deepcopy(self.row); row['ImportMap']['Stale']='changed'
        self.assertNotEqual(v3.project_package_stdout(wire([self.row])),v3.project_package_stdout(wire([row])))

    def test_invalid_types_for_every_registry_field(self):
        for producer,fields in v3.REGISTRY['types'].items():
            for field in fields:
                for bad in (0,1.5,None,[],{}):
                    if fields[field]=='[]string|null' and bad is None: continue
                    if fields[field] in ('[]string','[]string|null') and bad==[] and type(bad) is list: continue
                    if fields[field]=='[]*PackageError' and type(bad) is list: continue
                    if fields[field]=='map[string]string' and type(bad) is dict: continue
                    if fields[field].startswith('*') and fields[field]!='*time.Time' and type(bad) is dict: continue
                    with self.subTest(producer=producer,field=field,bad=bad),self.assertRaises(ValueError): v3._object({field:bad},producer)
        for field,bad in [('Stale','true'),('Stale',1),('StaleReason',False),('StaleReason',[])]:
            with self.subTest(field=field,bad=bad),self.assertRaises(ValueError): v3.project_package_stdout(wire([{field:bad}]))

    def test_duplicates_nonfinite_and_malformed_streams(self):
        for raw in (b'',b'[]',b'{}junk',b'{',b'{}\nnull',b'{"Stale":true,"Stale":false}',b'{"Module":{"Path":"a","Path":"b"}}',b'{"ImportMap":{"a":"b","a":"c"}}',b'{"Doc":NaN}',b'{"Doc":Infinity}',b'{"Doc":-Infinity}',b'{"Doc":1e999}',b'\xff'):
            with self.subTest(raw=raw),self.assertRaises(ValueError): v3.project_package_stdout(raw)

    def test_registry_source_shape_and_time(self):
        self.assertEqual(len(v3.REGISTRY['types']['PackagePublic']),58)
        self.assertEqual(len(v3.REGISTRY['types']['ModulePublic']),19)
        self.assertEqual(len(v3.REGISTRY['types']['Origin']),8)
        for t in ('bad','2026-02-30T00:00:00Z','2026-10-04','2026-10-04T25:00:00Z'):
            with self.assertRaises(ValueError): v3._object({'Time':t},'ModulePublic')
        v3._object({'ImportStack':None,'Pos':'','Err':'error'},'PackageError')


class EnvelopeTests(unittest.TestCase):
    def receipt(self):
        commands=[]
        for role in ('unbound_dependency_hydration_not_attested','attested_observation'):
            commands.append(dict(role=role,argv=['go','list'],exit=0,raw_stdout_sha256='a'*64,
                raw_stdout_bytes=10,binding_stdout_sha256='b'*64,binding_stdout_domain='package',stderr_sha256='c'*64,stderr_bytes=0))
        return v3._seal(dict(policy=v3.POLICY,schema_version=3,base_source={'build_context':v3.CONTEXT},
                            observation_envelope=commands))

    def test_external_pin_required_and_resigning_rejected(self):
        original=self.receipt(); pin=v3.observation_digest(original)
        v3._verify_receipt(original,pin)
        for field in ('raw_stdout_sha256','raw_stdout_bytes','stderr_sha256','stderr_bytes','argv','exit','role','binding_stdout_sha256'):
            for index in (0,1):
                changed=copy.deepcopy(original); command=changed['observation_envelope'][index]
                command[field]=["other"] if field=='argv' else (1 if field in ('exit','stderr_bytes','raw_stdout_bytes') else 'changed')
                changed=v3._seal(changed)
                with self.subTest(index=index,field=field),self.assertRaisesRegex(ValueError,'pin mismatch'): v3._verify_receipt(changed,pin)
        changed=copy.deepcopy(original);changed['observation_envelope'].reverse();changed=v3._seal(changed)
        with self.assertRaises(ValueError):v3._verify_receipt(changed,pin)
        for pin in (None,'',42,'a'*63):
            with self.assertRaises(ValueError):v3._verify_receipt(original,pin)
        with self.assertRaises(TypeError):v3.validate(original,Path('/unused'),'go',cache='c',modulecache='m')

    def test_observation_change_can_preserve_binding_and_stays_separately_pinned(self):
        a=self.receipt();b=copy.deepcopy(a)
        b['observation_envelope'][1]['raw_stdout_sha256']='d'*64
        b['observation_envelope'][1]['raw_stdout_bytes']=99;b=v3._seal(b)
        self.assertEqual(a['binding_sha256'],b['binding_sha256'])
        self.assertNotEqual(a['observation_sha256'],b['observation_sha256'])
        for receipt in (a,b):v3._verify_receipt(receipt,receipt['observation_sha256'])
        with self.assertRaises(ValueError):v3._verify_receipt(b,a['observation_sha256'])

    def test_version_rejection_before_capture_no_fallback(self):
        for version in (1,2,True):
            r=self.receipt();r['schema_version']=version
            with mock.patch.object(v3,'capture') as capture,self.assertRaises(ValueError):
                v3.validate(r,'root','go',cache='c',modulecache='m',trusted_observation_sha256='a'*64)
            capture.assert_not_called()
        r=self.receipt()
        with mock.patch.object(v2,'capture') as capture,self.assertRaises(ValueError):v2.validate(r,'root','go',cache='c',modulecache='m')
        capture.assert_not_called()

    def test_validation_fresh_binding_mismatch_and_external_pin_precedes_execution(self):
        r=self.receipt(); pin=r['observation_sha256']
        with mock.patch.object(v3,'capture',return_value=r):self.assertEqual(v3.validate(r,'root','go',cache='c',modulecache='m',trusted_observation_sha256=pin),r)
        for field in ('source','archive','coverage','replacement','contexts','package'):
            changed=copy.deepcopy(r);changed[field]='drift';changed=v3._seal(changed)
            with mock.patch.object(v3,'capture',return_value=changed),self.subTest(field=field),self.assertRaisesRegex(ValueError,'drifted'):
                v3.validate(r,'root','go',cache='c',modulecache='m',trusted_observation_sha256=pin)
        changed=copy.deepcopy(r);changed['observation_envelope'][0]['exit']=7;changed=v3._seal(changed)
        with mock.patch.object(v3,'capture') as capture,self.assertRaises(ValueError):v3.validate(changed,'root','go',cache='c',modulecache='m',trusted_observation_sha256=pin)
        capture.assert_not_called()


class CaptureOrderingTests(unittest.TestCase):
    """Synthetic command executor; actual authority predicates are tested separately."""
    def exercise(self, mutation=None, fail=None, source_drift=False):
        with tempfile.TemporaryDirectory() as tmp, ExitStack() as stack:
            root=Path(tmp);go=root/'go';go.write_bytes(b'fixture producer')
            base=dict(files={'scripts/r5_selected_source_binding_v2.py':'v2'},archive_static={},
                      archive_boundary={'production_variant_directories':[]},embed_inputs={})
            env=dict(GOVERSION='go1.25.7',GOOS='linux',GOARCH='amd64',GOHOSTOS='linux',GOHOSTARCH='amd64',
                     CGO_ENABLED='1',GOWORK='off',GOENV='',GOFLAGS='',GOEXPERIMENT='',GOAMD64='v1',GOTOOLCHAIN='local',
                     GOGCCFLAGS='-fdebug-prefix-map=/tmp/go-build123=',GOCACHE='/tmp/cache',GOROOT='/tmp/go',GOMODCACHE='/tmp/mod')
            calls=[];package_count=0
            def run(argv,**kwargs):
                nonlocal package_count
                calls.append(argv)
                if fail and len(calls)==2:
                    if isinstance(fail,Exception):raise fail
                    return subprocess.CompletedProcess(argv,7,b'partial',b'failure')
                if argv[1]=='env':raw=json.dumps(env).encode()
                elif '-m' in argv:raw=b'{"Path":"tabmail","Main":true}'
                else:
                    package_count+=1;row={'ImportPath':'fixture','Dir':'/outside','Stale':True,'StaleReason':'before'}
                    if mutation:mutation(row,package_count)
                    raw=wire([row])
                return subprocess.CompletedProcess(argv,0,raw,b'')
            def hashes(root,names):
                return {n:(v3._IMPLEMENTATION_SHA256 if n==v3._V3_FILES[0] else
                           v3._sha(v3._REGISTRY_BYTES) if n==v3.SCHEMA_PATH else
                           v2._IMPLEMENTATION_SHA256 if n.endswith('v2.py') else 'f'*64) for n in names}
            stack.enter_context(mock.patch.object(v3,'GO_SHA256',v3._sha(go.read_bytes())))
            stack.enter_context(mock.patch.object(v3.subprocess,'run',side_effect=run))
            stack.enter_context(mock.patch.object(v3.inventory,'capture_current_source',side_effect=[base,dict(base,drift=True)] if source_drift else [base,base]))
            stack.enter_context(mock.patch.object(v3,'digest',side_effect=hashes))
            stack.enter_context(mock.patch.object(v3,'topology',return_value=set()))
            stack.enter_context(mock.patch.object(v3,'classify',return_value=({},[],[],[],[])))
            stack.enter_context(mock.patch.object(v3,'normalize_mvs',return_value=[]))
            stack.enter_context(mock.patch.object(v3,'compare_coverage',return_value={}))
            stack.enter_context(mock.patch.object(v3,'compare_variants',return_value=[]))
            return v3.capture(root,go,cache=root/'cache',modulecache=root/'mod'),calls

    def test_all_package_roles_projected_including_hydration_and_repetitions(self):
        receipt,calls=self.exercise(lambda row,n:row.update(Stale=bool(n%2),StaleReason=str(n)))
        self.assertEqual(len(calls),12)
        self.assertEqual(receipt['observation_envelope'][1]['role'],'unbound_dependency_hydration_not_attested')
        package=[r for r in receipt['observation_envelope'] if '-m' not in r['argv'] and r['argv'][1]=='list']
        self.assertEqual(len(package),9)
        self.assertEqual(len({r['binding_stdout_sha256'] for r in package}),1)
        self.assertEqual(len({r['raw_stdout_sha256'] for r in package}),9)

    def test_retained_mutation_in_each_repeated_role_rejected(self):
        for n in (6,7,8,9):
            with self.subTest(role=n),self.assertRaisesRegex(ValueError,'outside diagnostic'):
                self.exercise(lambda row,count:row.update(Imports=['changed']) if count==n else None)

    def test_unknown_or_diagnostic_type_in_every_package_role_rejected(self):
        for n in range(1,10):
            for field,value in [('Unknown',True),('Stale','bad')]:
                with self.subTest(role=n,field=field),self.assertRaises(ValueError):
                    self.exercise(lambda row,count:row.update({field:value}) if count==n else None)

    def test_source_before_after_drift_rejected(self):
        with self.assertRaisesRegex(ValueError,'source/hash/topology'):self.exercise(source_drift=True)

    def test_producer_failure_interface_and_timeout_preserved(self):
        with self.assertRaises(v3.MetadataCommandFailure) as caught:self.exercise(fail=True)
        self.assertEqual(caught.exception.stdout,b'partial');self.assertEqual(caught.exception.stderr,b'failure')
        self.assertEqual(caught.exception.command['exit'],7)
        self.assertEqual(caught.exception.command['role'],'unbound_dependency_hydration_not_attested')
        error=subprocess.TimeoutExpired(['go'],180,output=b'partial',stderr=b'failure')
        with self.assertRaises(subprocess.TimeoutExpired) as caught:self.exercise(fail=error)
        self.assertIs(caught.exception,error)


if __name__=='__main__':unittest.main()
