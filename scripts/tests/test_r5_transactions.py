import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('r5_transactions', Path(__file__).resolve().parents[1] / 'check_r5_transactions.py')
tx = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tx)


class TransactionInventoryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data = json.loads(tx.CATALOG.read_text())
        cls.ast = tx.extract()
        cls.migrations = tx.migration_inventory()

    def rejected(self, data=None, ast=None, migrations=None):
        with self.assertRaises(ValueError):
            tx.validate(data or self.data, ast or self.ast, migrations or self.migrations)

    def test_current_inventory_is_only_structure_evidence(self):
        result = tx.validate(self.data, self.ast, self.migrations)
        self.assertEqual(result['functions'], 395)
        self.assertEqual(result['postgres_files'], 62)
        self.assertEqual(result['migration_files'], 19)
        self.assertFalse(result['runtime_verified'])
        self.assertFalse(result['task_complete'])

    def test_missing_function_rejected(self):
        data=copy.deepcopy(self.data); data['entries'].pop(); self.rejected(data=data)

    def test_duplicate_entry_rejected(self):
        data=copy.deepcopy(self.data); data['entries'].append(data['entries'][0]); self.rejected(data=data)

    def test_new_write_function_rejected(self):
        ast=copy.deepcopy(self.ast)
        f=copy.deepcopy(next(f for f in ast['functions'] if f['file'].startswith(tx.PG)))
        f.update(id=tx.PG+'future.go:*PgStore:FutureWrite',name='FutureWrite',file=tx.PG+'future.go')
        f['strings']=[{'line':1,'value':'UPDATE users SET is_active=false'}]
        ast['functions'].append(f); self.rejected(ast=ast)

    def test_new_file_without_functions_rejected(self):
        ast=copy.deepcopy(self.ast)
        ast['files'].append({'path':tx.PG+'future.go','sha256':'0'*64,'strings':[]}); self.rejected(ast=ast)

    def test_new_write_in_old_function_rejected(self):
        ast=copy.deepcopy(self.ast)
        f=next(f for f in ast['functions'] if f['file'].startswith(tx.PG))
        f['strings'].append({'line':1,'value':'DELETE FROM tenants'})
        self.rejected(ast=ast)

    def test_package_sql_constant_drift_rejected(self):
        ast=copy.deepcopy(self.ast)
        next(f for f in ast['files'] if f['path'].startswith(tx.PG))['sha256']='1'*64
        self.rejected(ast=ast)

    def test_callback_signature_drift_rejected(self):
        ast=copy.deepcopy(self.ast)
        next(f for f in ast['functions'] if f['name']=='SaveParsedMessage')['params']+=' func(context.Context) error'
        self.rejected(ast=ast)

    def test_external_caller_change_rejected(self):
        ast=copy.deepcopy(self.ast)
        next(f for f in ast['functions'] if not f['file'].startswith(tx.PG))['calls'].append({'line':1,'name':'SaveParsedMessage','expr':'store.SaveParsedMessage'})
        self.rejected(ast=ast)

    def test_fk_or_trigger_drift_rejected(self):
        migrations=copy.deepcopy(self.migrations)
        migrations[0]['definitions'].append({'line':1,'child_context':'x','source':'REFERENCES users(id)'})
        self.rejected(migrations=migrations)

    def test_new_migration_rejected(self):
        migrations=copy.deepcopy(self.migrations)
        migrations.append({'path':tx.PG+'migrations/future.sql','sha256':'2'*64,'definitions':[]})
        self.rejected(migrations=migrations)

    def test_missing_file_type_review_rejected(self):
        data=copy.deepcopy(self.data);data['reviewed_file_types'].pop(next(iter(data['reviewed_file_types'])))
        self.rejected(data=data)

    def test_missing_specific_assertion_rejected(self):
        data=copy.deepcopy(self.data);data['entries'][0]['assertions']={};self.rejected(data=data)

    def test_missing_specific_sql_effect_rejected(self):
        data=copy.deepcopy(self.data)
        e=next(e for e in data['entries'] if e['classification']['direct_write'])
        e['assertions']['direct_sql_effects']=[];self.rejected(data=data)

    def test_missing_callback_effect_rejected(self):
        data=copy.deepcopy(self.data)
        e=next(e for e in data['entries'] if e['entry']=='CreateMessageWithQuota')
        e['assertions']['callback_effect']='';self.rejected(data=data)

    def test_structure_cannot_claim_runtime_or_parent_acceptance(self):
        for key in ['task_complete','runtime_verified']:
            data=copy.deepcopy(self.data);data[key]=True;self.rejected(data=data)

    def test_update_detected_but_row_lock_not_mutation(self):
        self.assertTrue(tx.MUTATION.search('UPDATE users SET is_active=false'))
        self.assertTrue(tx.MUTATION.search('UPDATE outbound_jobs j SET state=$1'))
        self.assertFalse(tx.MUTATION.search('SELECT id FROM users FOR UPDATE SKIP LOCKED'))

    def test_literal_and_concatenated_dynamic_sql_distinguished(self):
        self.assertTrue(tx.is_literal('`SELECT id FROM users`'))
        self.assertTrue(tx.is_literal('"SELECT id FROM users"'))
        self.assertFalse(tx.is_literal('`SELECT id FROM users` + suffix'))
        self.assertFalse(tx.is_literal('query'))

    def test_cache_has_specific_message_fence_not_ancestor_lock(self):
        e=next(e for e in self.data['entries'] if e['entry']=='SaveParsedMessage')
        self.assertIn('message SHARE NOWAIT',e['lock_fk_wait_fence']['boundary'])
        self.assertEqual(e['evidence_level'],'source-only')
        self.assertIn('no new ancestor tenant lock',e['lock_fk_wait_fence']['boundary'])
        self.assertIn('not executed against PG',e['source_review']['unverified'])


    def entry(self, name):
        return next(e for e in self.data['entries'] if e['entry'] == name)

    def test_pr23_each_added_and_changed_body_has_independent_source_review(self):
        evidence = tx.CATALOG.parent / 'PR23-TRANSACTION-INVENTORY-20261003/function-review.json'
        review = json.loads(evidence.read_text())
        self.assertEqual((review['added'], review['removed'], review['body_changed']), (45, 2, 33))
        entries = {e['id']: e for e in self.data['entries']}
        self.assertEqual(len(review['functions']), 78)
        self.assertEqual(len({r['id'] for r in review['functions']}), 78)
        for row in review['functions']:
            with self.subTest(function=row['id']):
                e = entries[row['id']]
                self.assertEqual(e['source_review'], {k: row[k] for k in
                    ('role', 'trace', 'unverified', 'source_sha256', 'base_commit')})
                self.assertEqual(e['syntax']['sha256'], row['source_sha256'])
                self.assertEqual(e['classification'], row['classification'])
                self.assertEqual(e['evidence_level'], 'source-only')
        for row in review['removed_functions']:
            self.assertNotIn(row['id'], entries)
        self.assertFalse(review['runtime_verified'])

    def test_obsolete_function_and_wrong_receiver_rejected(self):
        for identity in [tx.PG+'submissions.go::loadSubmissionRecipients',
                         tx.PG+'company_mail.go:*PgStore:check']:
            data = copy.deepcopy(self.data)
            e = copy.deepcopy(self.entry('check')); e['id'] = identity
            data['entries'].append(e)
            with self.assertRaisesRegex(ValueError, 'postgres function set drift:.*obsolete='):
                tx.validate(data, self.ast, self.migrations)

    def test_constant_backed_observation_is_write_despite_ast_heuristic(self):
        e = self.entry('touchAPIKeyObservation')
        # The unchanged producer does not resolve package SQL constants. Its
        # read/dynamic heuristic must never be used as a transaction allowlist.
        self.assertFalse(e['classification']['direct_write'])
        self.assertEqual(e['classification']['kind'], 'dynamic-sql-review')
        self.assertEqual(e['source_review']['role'], 'autocommit-observation-write')
        self.assertEqual(e['assertions']['sql_execution_expressions'][0]['sql_expr'], 'apiKeyUsageUpdateSQL')
        pgfile = next(f for f in self.ast['files'] if f['path'] == e['syntax']['file'])
        self.assertTrue(any(tx.MUTATION.search(s['value']) and 'tenant_api_key_usage' in s['value']
                            for s in pgfile['strings']))
        ast = copy.deepcopy(self.ast)
        next(f for f in ast['files'] if f['path'] == pgfile['path'])['sha256'] = 'a'*64
        with self.assertRaisesRegex(ValueError, 'postgres file set/content drift'):
            tx.validate(self.data, ast, self.migrations)

    def test_read_preview_and_sse_have_real_lock_and_callback_boundaries(self):
        preview = self.entry('GetPermissionProfileDeletionPreview')
        self.assertTrue(preview['classification']['explicit_lock'])
        self.assertFalse(preview['classification']['direct_write'])
        self.assertEqual(preview['source_review']['role'], 'authorized-transaction-locking-reader')
        self.assertIn('UPDATE NOWAIT', preview['source_review']['trace'])
        members = self.entry('permissionProfileMembersTx')
        self.assertIn('NO KEY UPDATE NOWAIT', members['source_review']['trace'])
        self.assertIn('tenant KEY SHARE NOWAIT', members['source_review']['trace'])
        callback = self.entry('WithCompanyAdminEventAccess')
        self.assertTrue(callback['classification']['callback_parameter'])
        self.assertEqual(callback['source_review']['role'], 'transaction-external-callback')
        self.assertIn('cannot be rolled back', callback['assertions']['callback_effect'])
        data = copy.deepcopy(self.data)
        next(e for e in data['entries'] if e['entry'] == callback['entry'])['assertions']['callback_effect'] = ''
        self.rejected(data=data)

    def test_gc_round_and_receipt_projection_are_not_transaction_acceptance(self):
        scheduler = self.entry('sweepCompanyAttachmentTenants')
        self.assertEqual(scheduler['source_review']['role'], 'session-lock-scheduler')
        self.assertIn('autocommits', scheduler['source_review']['trace'])
        sweep = self.entry('sweepCompanyAttachmentsWith')
        self.assertEqual(sweep['source_review']['role'], 'transaction-owner-gc')
        states = self.entry('loadSubmissionReceiptRecipients')
        sql = states['assertions']['sql_execution_expressions'][0]['sql_expr']
        self.assertIn('SELECT job_id,state', sql)
        self.assertNotIn('SELECT job_id,address', sql)
        self.assertEqual(self.entry('permissionFieldParam')['source_review']['role'], 'pure-parameter-builder')
        # Interface QueryRow is a querier, even though the old callback syntax
        # heuristic over-approximates the embedded func signature.
        self.assertIn('not a function callback', self.entry('effectivePermissionPolicy')['assertions']['callback_effect'])

    def test_new_pure_function_is_also_rejected_and_no_inventory_allowlist(self):
        ast = copy.deepcopy(self.ast)
        f = copy.deepcopy(self.entry('permissionPositiveRevision')['syntax'])
        f.update(id=tx.PG+'future.go::FuturePure', file=tx.PG+'future.go', name='FuturePure', calls=[], strings=[])
        ast['functions'].append(f)
        with self.assertRaisesRegex(ValueError, 'postgres function set drift: missing='):
            tx.validate(self.data, ast, self.migrations)

    def test_current_trigger_and_package_constant_changes_remain_fenced(self):
        self.assertTrue(any('CREATE TRIGGER permission_override_revision' in d['source']
                            for m in self.migrations for d in m['definitions']))
        migrations = copy.deepcopy(self.migrations)
        trigger = next(m for m in migrations if any('permission_override_revision' in d['source']
                                                   for d in m['definitions']))
        trigger['definitions'] = []
        with self.assertRaisesRegex(ValueError, 'migration/FK/trigger definition drift'):
            tx.validate(self.data, self.ast, migrations)
        data = copy.deepcopy(self.data)
        next(e for e in data['entries'] if e['entry'] == 'applyPermissionPatchTx')['classification']['direct_write'] = False
        with self.assertRaisesRegex(ValueError, 'classification drift'):
            tx.validate(data, self.ast, self.migrations)

if __name__=='__main__':
    unittest.main()
