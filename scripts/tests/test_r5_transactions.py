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
        self.assertEqual(result['functions'], 352)
        self.assertEqual(result['postgres_files'], 49)
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
        self.assertIn('message SHARE NOWAIT',e['lock_fk_wait_fence'])
        self.assertEqual(e['evidence_level'],'actual-batch-linked-relationship')
        self.assertIn('不新增T锁',e['lock_fk_wait_fence'])

if __name__=='__main__':
    unittest.main()
