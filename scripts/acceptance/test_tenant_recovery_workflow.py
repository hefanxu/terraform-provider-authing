import re
import hashlib
import unittest
from pathlib import Path
from unittest.mock import Mock, patch
import subprocess

import run_live_case as live

ROOT = Path(__file__).resolve().parents[2]


class TenantRecoveryWorkflowTests(unittest.TestCase):
    def test_exact_read_only_incident_has_separate_job(self):
        workflow = (ROOT / '.github/workflows/authing-acceptance.yml').read_text()
        self.assertIn('          - tenant\n', workflow.split('      read_only_audit:', 1)[1])
        job = workflow.split('\n  tenant-recovery:\n', 1)[1].split('\n  destructive-case:', 1)[0]
        for gate in ("inputs.confirm == 'READ_ONLY_SANDBOX'", "inputs.read_only_audit == 'tenant'",
                     "inputs.test_case == 'group'", "inputs.test_object_code == 'hermesacc-97f07719b2e15f0c'",
                     "inputs.audit_start == ''", "inputs.audit_end == ''", 'refs/heads/feat/management-api-coverage'):
            self.assertIn(gate, job)
        for required in ('environment: authing-sandbox', 'AUTHING_TENANT_RECOVERY_NAME:',
                         "^TestReadOnlySandboxTenantRecovery$", '-authing-tenant-recovery-sandbox'):
            self.assertIn(required, job)
        self.assertNotIn('DESTRUCTIVE_SANDBOX', job)
        self.assertNotIn('run_live_case.py', job)

    def test_other_legacy_recovery_jobs_are_narrowed(self):
        workflow = (ROOT / '.github/workflows/authing-acceptance.yml').read_text()
        for name in ('application-recovery', 'post-recovery', 'namespace-recovery', 'destructive-case', 'namespace-recovery-cleanup'):
            job = workflow.split('\n  ' + name + ':\n', 1)[1].split('\n  ', 1)[0]
            self.assertIn("inputs.read_only_audit == 'none'", job)
            self.assertIn("inputs.audit_start == ''", job)
            self.assertIn("inputs.audit_end == ''", job)

    def test_tenant_and_dependencies_remain_unselectable(self):
        for case in ('tenant', 'tenant_membership', 'tenant_organization'):
            self.assertNotIn(case, live.CASES)

    def test_typed_tenant_diagnostic_survives_outer_allowlist_without_raw_output(self):
        test = 'TestDestructiveLiveTenantTrace'
        stage = 'stage=apps category=invalid-shape http_error=0 business=200 api_code=0 shape=missing-or-invalid-appids pages=0 count=-1 matches=0 total=-1'
        pinned = 'state_id_sha256=' + 'a' * 64
        output = 'tenant phase=verify-created code=hermesacc-1234567890abcdef (output suppressed) ' + stage + ' cleanup=incomplete ' + pinned + ' secret-marker raw-id-marker'
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + '\n', ''),
                                   subprocess.CompletedProcess([], 1, output, 'credential-marker')])
        env = {'ACCEPTANCE_TEST_CASE': 'tenant', 'AUTHING_ACCEPTANCE_CONFIRM': 'DESTRUCTIVE_SANDBOX',
               'AUTHING_ACCESS_KEY_ID': 'credential-marker', 'AUTHING_ACCESS_KEY_SECRET': 'secret-marker'}
        with patch.dict(live.CASES, {'tenant': test}), self.assertRaises(RuntimeError) as caught:
            live.run(env, runner)
        self.assertIn(stage, str(caught.exception))
        self.assertIn(pinned, str(caught.exception))
        self.assertIn('verify-created', str(caught.exception))
        self.assertIn('cleanup=incomplete', str(caught.exception))
        for private in ('secret-marker', 'credential-marker', 'raw-id-marker'):
            self.assertNotIn(private, str(caught.exception))

    def test_actual_organization_incident_failure_survives_outer_allowlist(self):
        result = subprocess.run(['go', 'test', '-count=1', '-v', '-run',
                                 '^TestTenantOrganizationIncidentKeepsPinnedEvidenceWithoutDeleting$',
                                 './scripts/acceptance'], cwd=ROOT, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, 'offline tenant incident regression failed')
        result.returncode = 1  # Replay only the real captured mock trace failure.
        test = 'TestDestructiveLiveTenantTrace'
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + '\n', ''), result])
        env = {'ACCEPTANCE_TEST_CASE': 'tenant', 'AUTHING_ACCEPTANCE_CONFIRM': 'DESTRUCTIVE_SANDBOX',
               'AUTHING_ACCESS_KEY_ID': 'credential-marker', 'AUTHING_ACCESS_KEY_SECRET': 'secret-marker'}
        with patch.dict(live.CASES, {'tenant': test}), self.assertRaises(RuntimeError) as caught:
            live.run(env, runner)
        for expected in ('verify-created', 'cleanup=incomplete', 'stage=organizations category=nonempty',
                         'state_id_sha256=' + hashlib.sha256(b'tenant-unique-id').hexdigest()):
            self.assertIn(expected, str(caught.exception))
        for private in ('tenant-unique-id', 'credential-marker', 'secret-marker', 'mock-token'):
            self.assertNotIn(private, str(caught.exception))


if __name__ == '__main__':
    unittest.main()
