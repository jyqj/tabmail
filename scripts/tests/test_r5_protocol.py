import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('r5_protocol', Path(__file__).resolve().parents[1] / 'check_r5_protocol.py')
protocol = importlib.util.module_from_spec(spec)
spec.loader.exec_module(protocol)


class CaseStructureTests(unittest.TestCase):
    def setUp(self):
        self.data = protocol.load_cases()

    def check_mutation(self, modify):
        data = copy.deepcopy(self.data)
        modify(data)
        with tempfile.TemporaryDirectory() as tmp:
            file = Path(tmp) / 'cases.json'
            file.write_text(json.dumps(data))
            with self.assertRaises(ValueError):
                protocol.load_cases(file)

    def test_full_case_set_does_not_claim_completion(self):
        report = protocol.summary(self.data)
        self.assertEqual(report['cases'], 46)
        self.assertEqual(report['shared_input_verified_cases'], 0)
        self.assertFalse(report['task_complete'])
        self.assertEqual(len(report['missing_shared_input_adapters']), 0)

    def test_missing_case_rejected(self):
        self.check_mutation(lambda d: d['cases'].pop())

    def test_duplicate_case_rejected(self):
        self.check_mutation(lambda d: d['cases'].append(d['cases'][0]))

    def test_empty_input_rejected(self):
        self.check_mutation(lambda d: d['cases'][0].update(input={}))

    def test_completion_claim_rejected(self):
        self.check_mutation(lambda d: d.update(task_complete=True))

    def test_invalid_status_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['expected'].update(http_status=999))

    def test_null_reason_is_not_a_protocol_gap(self):
        data = copy.deepcopy(self.data)
        data['cases'][0]['unresolved'] = []
        with tempfile.TemporaryDirectory() as tmp:
            file = Path(tmp)/'cases.json'; file.write_text(json.dumps(data))
            protocol.load_cases(file)

    def test_missing_error_code_rejected(self):
        self.check_mutation(lambda d: d['cases'][1]['expected']['wire_error'].update(code=None))

    def test_missing_message_requirement_rejected(self):
        self.check_mutation(lambda d: d['cases'][1]['expected']['wire_error'].update(message_mode='optional'))

    def test_exact_reason_requires_literal(self):
        self.check_mutation(lambda d: d['cases'][1]['expected']['wire_error'].update(reason_mode='exact'))

    def test_absent_reason_cannot_have_value(self):
        self.check_mutation(lambda d: d['cases'][1]['expected']['wire_error'].update(reason='invented'))

    def test_reference_adapter_cannot_claim_shared_input(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(consumes_shared_input=True))

    def test_nonexistent_real_test_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(test='TestInventedProtocolPass'))

    def test_wrong_package_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(package='./internal/api'))

    def test_path_escape_rejected(self):
        self.check_mutation(lambda d: d['cases'][0]['reference_adapters'][0].update(source='../outside.go'))

    def test_audit_and_acceptance_coverage(self):
        self.assertEqual({a for row in self.data['cases'] for a in row['audit']}, {f'A{i:02d}' for i in range(1,8)})
        self.assertTrue(all(row['acceptance'] for row in self.data['cases']))


class ReferenceEvidenceTests(unittest.TestCase):
    package = 'tabmail/internal/store/postgres'
    name = 'TestRealProtocolReference'

    def events(self):
        return [{'Package':self.package,'Action':'start'},
                {'Package':self.package,'Action':'run','Test':self.name},
                {'Package':self.package,'Action':'pass','Test':self.name},
                {'Package':self.package,'Action':'pass'}]

    def classify(self, rows, code=0):
        return protocol.classify_events('\n'.join(map(json.dumps, rows)),self.package,{self.name},code)

    def test_named_pass_remains_reference_only(self):
        report = self.classify(self.events())
        self.assertFalse(report['errors'])
        self.assertFalse(report['task_complete'])
        self.assertEqual(report['status'], 'reference_tests_passed')

    def test_skip_rejected(self):
        events = self.events(); events[2]['Action'] = 'skip'
        self.assertTrue(self.classify(events)['errors'])

    def test_baseline_failure_is_not_protocol_pass(self):
        events = self.events(); events[2]['Action'] = 'fail'; events[-1]['Action'] = 'fail'
        self.assertTrue(self.classify(events, 1)['errors'])

    def test_missing_test_rejected(self):
        self.assertTrue(self.classify([self.events()[-1]])['errors'])

    def test_duplicate_terminal_rejected(self):
        events = self.events(); events.insert(3, events[2].copy())
        self.assertTrue(self.classify(events)['errors'])

    def test_compile_failure_rejected(self):
        self.assertTrue(self.classify([{'Package':self.package,'Action':'fail'}],1)['errors'])

    def test_cached_pass_without_run_rejected(self):
        events = self.events(); del events[1]
        self.assertTrue(self.classify(events)['errors'])

    def test_subtest_failure_rejected(self):
        events = self.events(); events.insert(2, {'Package':self.package,'Action':'fail','Test':self.name+'/expired'})
        self.assertTrue(self.classify(events)['errors'])

    def test_unexpected_package_rejected(self):
        events = self.events(); events[0]['Package'] = 'tabmail/fake'
        self.assertTrue(self.classify(events)['errors'])

    def test_unexpected_test_rejected(self):
        events = self.events(); events[1]['Test'] = 'TestOther'
        self.assertTrue(self.classify(events)['errors'])

    def test_malformed_json_rejected(self):
        with self.assertRaises(ValueError):
            protocol.classify_events('not json',self.package,{self.name},0)

class SharedConsumerTests(unittest.TestCase):
    def test_declared_shared_consumers_are_real_sources(self):
        data = protocol.load_cases()
        report = protocol.summary(data)
        self.assertEqual(report['cases_with_shared_input_adapters'], 46)
        # Structure remains zero runtime verification.
        self.assertEqual(report['shared_input_verified_cases'], 0)

    def test_component_pass_exact_case_set(self):
        report = protocol.classify_components({'success':True,'testResults':[{'assertionResults':[
            {'fullName':'R5 shared receipt RC03','status':'passed'}]}]}, {'R5 shared receipt RC03'}, 0)
        self.assertFalse(report['errors'])
        self.assertFalse(report['task_complete'])

    def test_component_missing_or_skipped_rejected(self):
        for status in ['skipped','pending','failed']:
            report = protocol.classify_components({'success':True,'testResults':[{'assertionResults':[
                {'fullName':'R5 shared receipt RC03','status':status}]}]}, {'R5 shared receipt RC03'}, 0)
            self.assertTrue(report['errors'])
        self.assertTrue(protocol.classify_components({'success':True,'testResults':[]},{'R5 shared receipt RC03'},0)['errors'])

    def test_component_duplicate_or_nonzero_exit_rejected(self):
        row={'fullName':'R5 shared receipt RC03','status':'passed'}
        self.assertTrue(protocol.classify_components({'success':True,'testResults':[{'assertionResults':[row,row]}]}, {row['fullName']}, 0)['errors'])
        self.assertTrue(protocol.classify_components({'success':True,'testResults':[{'assertionResults':[row]}]}, {row['fullName']}, 1)['errors'])

    def test_machine_markdown_output_drift_rejected(self):
        data = protocol.load_cases()
        data['cases'][0]['expected']['assertion'] = 'silently changed policy'
        with tempfile.TemporaryDirectory() as tmp:
            file = Path(tmp)/'cases.json';file.write_text(json.dumps(data))
            with self.assertRaises(ValueError): protocol.load_cases(file)


if __name__ == '__main__':
    unittest.main()


class ExactTargetRedTests(unittest.TestCase):
    def classify(self, diagnostic='R5_PROTOCOL_TARGET_RT01: restore changed immutable hard expiry', code=1):
        p = 'tabmail/internal/store/postgres'
        parent = 'TestR5ProtocolRetentionSharedCases'
        leaf = parent+'/RT01/default'
        events = [{'Package':p,'Action':'run','Test':parent},
                  {'Package':p,'Action':'run','Test':leaf},
                  {'Package':p,'Action':'output','Test':leaf,'Output':'    r5_protocol_shared_test.go:123: '+diagnostic+'\n'},
                  {'Package':p,'Action':'fail','Test':leaf},
                  {'Package':p,'Action':'fail','Test':parent},
                  {'Package':p,'Action':'fail'}]
        return protocol.classify_shared_events('\n'.join(map(json.dumps,events)),p,{parent},code,{'cases':[r for r in protocol.load_cases()['cases'] if r['id']=='RT01']})

    def test_exact_target_red_is_baseline_not_product_green(self):
        report = self.classify()
        self.assertFalse(report['errors'])
        self.assertFalse(report['product_green'])
        self.assertEqual(report['process_exit_code'],1)
        self.assertFalse(report['task_complete'])
        self.assertTrue(report['target_red'])

    def test_setup_or_other_failure_not_admitted(self):
        self.assertTrue(self.classify('database fixture failed')['errors'])
        self.assertTrue(self.classify('R5_PROTOCOL_TARGET_RT01: okay\n    r5_protocol_shared_test.go:124: database failed')['errors'])

    def test_timeout_process_not_admitted(self):
        self.assertTrue(self.classify(code=124)['errors'])

    def test_parent_cleanup_failure_cannot_be_laundered(self):
        p='tabmail/internal/store/postgres'; parent='TestR5ProtocolRetentionSharedCases'; leaf=parent+'/RT01/default'
        base=[{'Package':p,'Action':'run','Test':parent},{'Package':p,'Action':'run','Test':leaf},
              {'Package':p,'Action':'output','Test':leaf,'Output':'    x_test.go:1: R5_PROTOCOL_TARGET_RT01: restore changed hard expiry\n'},
              {'Package':p,'Action':'fail','Test':leaf}]
        for scope,text in [(parent,'    x_test.go:2: cleanup database failed\n'),(None,'panic: cleanup failed\n'),
                           (parent,'runtime error: invalid memory address\n'),(None,'context deadline exceeded\n'),
                           (None,'# tabmail/internal/store/postgres\nx.go:12: compile failure\n')]:
            events=base+[{'Package':p,'Action':'output','Test':scope,'Output':text},
                         {'Package':p,'Action':'fail','Test':parent},{'Package':p,'Action':'fail'}]
            report=protocol.classify_shared_events('\n'.join(map(json.dumps,events)),p,{parent},1,{'cases':[r for r in protocol.load_cases()['cases'] if r['id']=='RT01']})
            self.assertTrue(report['errors'],text)

    def test_undeclared_target_variant_rejected(self):
        # The driver must not admit arbitrary children under a known case.
        p='tabmail/internal/store/postgres';parent='TestR5ProtocolRetentionSharedCases';leaf=parent+'/RT01/invented'
        events=[{'Package':p,'Action':'run','Test':parent},{'Package':p,'Action':'run','Test':leaf},
                {'Package':p,'Action':'output','Test':leaf,'Output':'    x_test.go:1: R5_PROTOCOL_TARGET_RT01: failure\n'},
                {'Package':p,'Action':'fail','Test':leaf},{'Package':p,'Action':'fail','Test':parent},{'Package':p,'Action':'fail'}]
        self.assertTrue(protocol.classify_shared_events('\n'.join(map(json.dumps,events)),p,{parent},1,{'cases':[r for r in protocol.load_cases()['cases'] if r['id']=='RT01']})['errors'])


class BaselineBuildTagTests(unittest.TestCase):
    def test_protocol_target_suite_is_explicitly_opt_in(self):
        data=protocol.load_cases()
        adapters=[a for row in data['cases'] for a in row.get('shared_adapters',[])
                  if a['source']=='internal/store/postgres/r5_protocol_shared_test.go']
        self.assertTrue(adapters)
        self.assertTrue(all(a.get('build_tag')=='r5protocol' for a in adapters))
        source=(protocol.ROOT/adapters[0]['source']).read_text()
        self.assertTrue(source.startswith('//go:build r5protocol\n'))
        cmd=protocol.shared_command('./internal/store/postgres','r5protocol',{'TestR5ProtocolRetentionSharedCases'})
        self.assertIn('-tags=r5protocol',cmd)
        self.assertIn('-count=1',cmd)

    def test_invalid_or_shell_build_tag_rejected(self):
        with self.assertRaises(ValueError):
            protocol.shared_command('./internal/store/postgres','r5protocol; false',{'TestReal'})


class SharedRuntimePathTests(unittest.TestCase):
    def test_shared_go_requires_all_actual_case_variants(self):
        data=protocol.load_cases()
        adapter=next(a for r in data['cases'] if r['id']=='CT02' for a in r['shared_adapters'])
        self.assertEqual(len(adapter['runtime_test_paths']),3)
        p='tabmail/internal/store/postgres';name=adapter['test']
        events=[{'Package':p,'Action':'run','Test':name},{'Package':p,'Action':'pass','Test':name},{'Package':p,'Action':'pass'}]
        scoped={'cases':[r for r in data['cases'] if r['id']=='CT02']}
        report=protocol.classify_shared_events('\n'.join(map(json.dumps,events)),p,{name},0,scoped)
        self.assertTrue(report['errors'])

    def test_parent_fail_with_non_test_source_diagnostic_rejected(self):
        data={'cases':[r for r in protocol.load_cases()['cases'] if r['id']=='RT01']}
        p='tabmail/internal/store/postgres';name='TestR5ProtocolRetentionSharedCases';leaf=name+'/RT01/default'
        events=[{'Package':p,'Action':'run','Test':name},{'Package':p,'Action':'run','Test':leaf},
                {'Package':p,'Action':'output','Test':leaf,'Output':'    protocol_test.go:1: R5_PROTOCOL_TARGET_RT01: restore gap\n'},
                {'Package':p,'Action':'fail','Test':leaf},
                {'Package':p,'Action':'output','Test':name,'Output':'    fixture.go:2: cleanup SQL failed\n'},
                {'Package':p,'Action':'fail','Test':name},{'Package':p,'Action':'fail'}]
        self.assertTrue(protocol.classify_shared_events('\n'.join(map(json.dumps,events)),p,{name},1,data)['errors'])


class ApplicableStatusTests(unittest.TestCase):
    def test_background_ports_do_not_require_fake_http(self):
        rows={r['id']:r for r in protocol.load_cases()['cases']}
        for name in ['RT07','GC01','GC02']:
            self.assertEqual(rows[name]['expected']['http_status_kind'],'not_applicable')
            self.assertIsNone(rows[name]['expected']['http_status'])
            self.assertEqual(rows[name]['required_layers'],['db'])
        self.assertEqual(protocol.summary(protocol.load_cases())['unresolved_codes'],[])

    def test_registration_still_does_not_certify_missing_components(self):
        report=protocol.summary(protocol.load_cases())
        self.assertIn('RC02',report['remaining_semantic_or_layer_gaps'])
        self.assertFalse(report['task_complete'])

class SharedInfrastructureCannotBeProductGreenTests(unittest.TestCase):
    def run_fixture(self, failure):
        from unittest.mock import patch
        import subprocess
        row=copy.deepcopy(next(r for r in protocol.load_cases()['cases'] if r['id']=='RT01'))
        data={'cases':[row]};parent='TestR5ProtocolRetentionSharedCases';leaf=parent+'/RT01/default';package='tabmail/internal/store/postgres'
        if failure=='compile':
            events=[{'Package':package,'Action':'output','Output':'# tabmail/internal/store/postgres\nfile.go:1: compile error\n'}, {'Package':package,'Action':'fail'}]
        elif failure=='missing_runtime_consumer':
            events=[{'Package':package,'Action':'run','Test':parent},{'Package':package,'Action':'pass','Test':parent},{'Package':package,'Action':'pass'}]
        else:
            diagnostic={'panic':'panic: invalid memory address','timeout':'panic: test timed out after 120s','unknown_error':'    fixture_test.go:1: unexpected SQL error'}[failure]
            events=[{'Package':package,'Action':'run','Test':parent},{'Package':package,'Action':'run','Test':leaf},
                    {'Package':package,'Action':'output','Test':leaf,'Output':diagnostic+'\n'},
                    {'Package':package,'Action':'fail','Test':leaf},{'Package':package,'Action':'fail','Test':parent},{'Package':package,'Action':'fail'}]
        def process(command,**kwargs):
            if command[0]=='git':return subprocess.CompletedProcess(command,0,'fixture-source\n','')
            return subprocess.CompletedProcess(command,0 if failure=='missing_runtime_consumer' else 1,'\n'.join(map(json.dumps,events)),'')
        with tempfile.TemporaryDirectory() as tmp,patch.object(protocol,'source_closure',return_value={}),patch.object(protocol.subprocess,'run',side_effect=process),patch.dict(protocol.os.environ,{'TABMAIL_TEST_DB_DSN':'disposable-unit-test-not-connected'}):
            return protocol.run_shared(data,'db',Path(tmp)/'fresh')

    def test_compile_panic_timeout_unknown_error_and_missing_runtime_are_not_green(self):
        for failure in ['compile','panic','timeout','unknown_error','missing_runtime_consumer']:
            with self.subTest(failure=failure):
                report=self.run_fixture(failure)
                self.assertEqual(report['status'],'shared_scoped_evidence_failed')
                self.assertFalse(report['product_green'])
                self.assertFalse(report['task_complete'])
                self.assertTrue(report['reports'][0]['errors'])

    def test_unregistered_matching_runtime_consumer_cannot_be_green(self):
        parent='TestUnregisteredSharedConsumer';package='tabmail/internal/store/postgres'
        events=[{'Package':package,'Action':'run','Test':parent},{'Package':package,'Action':'pass','Test':parent},{'Package':package,'Action':'pass'}]
        report=protocol.classify_shared_events('\n'.join(map(json.dumps,events)),package,{parent},0,{'cases':[]})
        self.assertTrue(report['errors'])
        self.assertFalse(report['product_green'])


class GoOwnedComponentAdapterTests(unittest.TestCase):
    def fixtures(self):
        row=copy.deepcopy(next(r for r in protocol.load_cases()['cases'] if r['id']=='RC01'))
        adapter=next(a for a in row['shared_adapters'] if a.get('component_source'))
        return row,adapter

    def test_exact_adapter_target_is_separate_from_original_db(self):
        row,adapter=self.fixtures()
        result=protocol.validate_adapter_targets(row,adapter)
        self.assertEqual(len(result),1)
        self.assertEqual(next(iter(result.values()))['marker'],'R5_PROTOCOL_UI_TARGET_RC01_BCC')

    def test_unknown_path_marker_audit_or_acceptance_rejected(self):
        for mutation in ['path','marker','audit','acceptance','task']:
            row,adapter=self.fixtures();path=next(iter(adapter['target_failures']))
            if mutation=='path':adapter['target_failures'][path+'/invented']=adapter['target_failures'].pop(path)
            else:adapter['target_failures'][path][mutation]='invented'
            with self.subTest(mutation=mutation),self.assertRaises(ValueError):protocol.validate_adapter_targets(row,adapter)

    def test_same_case_go_ui_target_does_not_replace_db_target(self):
        row=copy.deepcopy(next(r for r in protocol.load_cases()['cases'] if r['id']=='RC03'))
        adapter=next(a for a in row['shared_adapters'] if a.get('component_source'));path=next(iter(adapter['target_failures']));parent=adapter['test'];package='tabmail/'+adapter['package'].removeprefix('./')
        events=[{'Package':package,'Action':'run','Test':parent},{'Package':package,'Action':'run','Test':path},
                {'Package':package,'Action':'output','Test':path,'Output':'    producer_test.go:1: R5_PROTOCOL_UI_TARGET_RC03_BCC: actual receipt rendered private BCC\n'},
                {'Package':package,'Action':'fail','Test':path},{'Package':package,'Action':'fail','Test':parent},{'Package':package,'Action':'fail'}]
        original=copy.deepcopy(row['baseline_target_failure'])
        report=protocol.classify_shared_events('\n'.join(map(json.dumps,events)),package,{parent},1,{'cases':[row]})
        self.assertFalse(report['errors']);self.assertEqual(report['target_red'][path],'R5_PROTOCOL_UI_TARGET_RC03_BCC')
        self.assertEqual(row['baseline_target_failure'],original);self.assertFalse(report['product_green'])

    def test_no_real_packet_keeps_component_layer_unverified(self):
        row,adapter=self.fixtures()
        selected={(adapter['package'],adapter['build_tag']):{adapter['test']}}
        with tempfile.TemporaryDirectory() as tmp:
            reports,verified=protocol.classify_go_component_packets({'cases':[row]},selected,Path(tmp),'a'*64)
            self.assertFalse(verified);self.assertTrue(reports[0]['errors']);self.assertFalse(reports[0]['product_green'])
