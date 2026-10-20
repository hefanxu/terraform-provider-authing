import json
import subprocess
import unittest
from pathlib import Path
from unittest.mock import Mock

import run_cleanup_inventory as mod

ROOT=Path(__file__).resolve().parents[2]

class CleanupInventoryTests(unittest.TestCase):
    def test_sanitizer_rejects_raw_data_and_non_closed_values(self):
        for row in ({'kind':'candidate','family':'user','state':'private-marker'},
                    {'kind':'candidate','family':'user','state':'complete','userId':'private-id'},
                    {'kind':'candidate','family':'user','state':'complete','generated_code':'private-name'}):
            with self.assertRaises(ValueError):mod.safe_event(row)
    def test_sanitizer_retains_safe_fingerprints(self):
        row={'kind':'candidate','family':'user','state':'candidate-exact-identity','authority':'none','id_sha256':'a'*64,'generated_code':'hermesacc-1234567890abcdef','marker_matches':True,'count':1,'total':1}
        self.assertEqual(mod.safe_event(row),row)
    def test_guard_never_runs_destructive(self):
        runner=Mock()
        with self.assertRaises(ValueError):mod.run({'AUTHING_ACCEPTANCE_CONFIRM':'DESTRUCTIVE_SANDBOX'},runner)
        runner.assert_not_called()
    def test_workflow_live_writes_paused_and_separate_read_only_job(self):
        w=(ROOT/'.github/workflows/authing-acceptance.yml').read_text()
        destructive=w.split('\n  destructive-case:\n')[1].split('\n  ',1)[0]
        self.assertIn('false &&',destructive)
        job=w.split('\n  cleanup-inventory:\n')[1]
        for gate in ("inputs.confirm == 'READ_ONLY_SANDBOX'","inputs.read_only_audit == 'cleanup_inventory'",'environment: authing-sandbox','run_cleanup_inventory.py'):
            self.assertIn(gate,job)
        self.assertNotIn('run_live_case.py',job)
    def test_read_allowlist_matches_official_contract_snapshot(self):
        contract=json.loads((ROOT/'docs/cleanup/read-contract.json').read_text())['paths']
        source=(ROOT/'scripts/acceptance/cleanup_inventory_impl_test.go').read_text().split('var cleanupReadPaths',1)[1].split('\n}',1)[0]
        for path,method in __import__('re').findall(r'"(/api/v3/[a-z-]+)": "(GET|POST)"',source):
            self.assertIn(path,contract)
            self.assertIn(method.lower(),contract[path])
    def test_pipeline_uses_published_name_field(self):
        source=(ROOT/'scripts/acceptance/cleanup_inventory_live_test.go').read_text()
        self.assertRegex(source,r'Family: "pipeline_function"[^\n]+Name: "funcName"')

if __name__=='__main__':unittest.main()
