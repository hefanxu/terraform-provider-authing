import re
import contextlib
import io
import subprocess
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import run_live_case as mod


class SandboxDispatchTests(unittest.TestCase):
    def test_workflow_choices_map_to_existing_live_tests(self):
        root = Path(__file__).resolve().parents[2]
        workflow = (root / ".github/workflows/authing-acceptance.yml").read_text()
        options = workflow.split("        options:\n", 1)[1].split("\n      test_object_code:", 1)[0]
        choices = re.findall(r"^          - ([a-z_]+)$", options, re.MULTILINE)
        self.assertCountEqual(choices, mod.CASES)
        names = set()
        for source in Path(__file__).parent.glob("*_test.go"):
            names.update(re.findall(r"^func (TestDestructiveLive\w+)\(", source.read_text(), re.MULTILINE))
        for name in mod.CASES.values():
            self.assertIn(name, names)

    def env(self, case="group"):
        return {
            "ACCEPTANCE_TEST_CASE": case,
            "AUTHING_ACCEPTANCE_CONFIRM": "DESTRUCTIVE_SANDBOX",
            "AUTHING_ACCESS_KEY_ID": "credential-marker",
            "AUTHING_ACCESS_KEY_SECRET": "secret-marker",
        }

    def test_rejects_unknown_case_before_any_process(self):
        runner = Mock()
        with self.assertRaises(ValueError):
            mod.run(self.env("unknown"), runner)
        runner.assert_not_called()

    def test_rejects_missing_confirmation_and_secrets(self):
        for key in ("AUTHING_ACCEPTANCE_CONFIRM", "AUTHING_ACCESS_KEY_ID", "AUTHING_ACCESS_KEY_SECRET"):
            env = self.env()
            del env[key]
            runner = Mock()
            with self.assertRaises(ValueError):
                mod.run(env, runner)
            runner.assert_not_called()

    def test_rejects_missing_test_instead_of_false_green(self):
        runner = Mock(return_value=subprocess.CompletedProcess([], 0, "ok: no tests found", ""))
        with self.assertRaisesRegex(RuntimeError, "missing"):
            mod.run(self.env(), runner)
        self.assertEqual(runner.call_count, 1)

    def test_group_requires_stage_evidence(self):
        test = mod.CASES["group"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), subprocess.CompletedProcess([], 0, "ok", "")])
        with self.assertRaisesRegex(RuntimeError, "stage evidence"):
            mod.run(self.env(), runner)

    def test_group_rejects_incomplete_duplicate_foreign_and_unconfirmed_evidence(self):
        code = "hermesacc-1234567890abcdef"
        lines = [f"group phase={phase} code={code} result=passed" for phase in mod.GROUP_PHASES]
        complete = f"group phase=complete code={code} cleanup=confirmed state_id_sha256=" + "a" * 64
        invalid = [lines[:i] + lines[i + 1:] + [complete] for i in range(len(lines))]
        invalid += [lines + [lines[0], complete], [line.replace(code, "hermesacc-fedcba0987654321") if i == 2 else line for i, line in enumerate(lines)] + [complete], lines, lines + [complete.replace("confirmed", "unknown")]]
        for output in invalid:
            test = mod.CASES["group"]
            runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), subprocess.CompletedProcess([], 0, "\n".join(output), "")])
            with self.assertRaises(RuntimeError):
                mod.run(self.env(), runner)

    def test_group_readback_failure_diagnostics_survive_outer(self):
        test = mod.CASES["group"]
        output = "group phase=apply-empty-description code=hermesacc-1234567890abcdef cleanup=confirmed group_readback=identity description_readback=null business=200 api_code=0 secret-marker"
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), subprocess.CompletedProcess([], 1, output, "")])
        with self.assertRaises(RuntimeError) as caught:
            mod.run(self.env(), runner)
        for value in ("group_readback=identity", "description_readback=null", "business=200", "api_code=0"):
            self.assertIn(value, str(caught.exception))
        self.assertNotIn("secret-marker", str(caught.exception))

    def test_group_actual_failures_survive_outer(self):
        root = Path(__file__).resolve().parents[2]
        for go_test, phase, cleanup, pinned in (
            ("TestMockGroupUnknownCreateIDNeverDeletes", "apply-create", "unknown", False),
            ("TestMockGroupReconcileAndCleanupFailures/reconcile", "apply-reconcile", "confirmed", True),
            ("TestMockGroupReconcileAndCleanupFailures/cleanup", "apply-reconcile", "incomplete", True),
        ):
            result = subprocess.run(["go", "test", "-count=1", "-v", "-run", "^" + go_test + "$", "./scripts/acceptance"], cwd=root, capture_output=True, text=True, check=False)
            self.assertEqual(result.returncode, 0, "offline group failure injection failed")
            result.returncode = 1
            test = mod.CASES["group"]
            runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), result])
            with self.assertRaises(RuntimeError) as caught:
                mod.run(self.env(), runner)
            text = str(caught.exception)
            for value in (phase, "hermesacc-1234567890abcdef", "cleanup=" + cleanup):
                self.assertIn(value, text)
            self.assertEqual("state_id_sha256=" in text, pinned)
            for secret in ("credential-marker", "secret-marker", "mock-token", "key-marker"):
                self.assertNotIn(secret, text)

    def test_group_actual_success_survives_outer(self):
        root = Path(__file__).resolve().parents[2]
        result = subprocess.run(["go", "test", "-count=1", "-v", "-run", "^TestMockDestructiveGroupTrace$", "./scripts/acceptance"], cwd=root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, "offline group lifecycle failed")
        test = mod.CASES["group"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), result])
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            mod.run(self.env(), runner)
        for phase in mod.GROUP_PHASES:
            self.assertIn("phase=" + phase, output.getvalue())
        self.assertIn("cleanup=confirmed state_id_sha256=", output.getvalue())

    def test_success_invokes_exact_test_and_flag(self):
        test = mod.CASES["application"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 0, "ok", "")])
        mod.run(self.env("application"), runner)
        self.assertEqual(runner.call_count, 2)
        argv = runner.call_args.args[0]
        self.assertEqual(argv[-2:], ["-args", "-authing-destructive-sandbox"])
        self.assertEqual(argv[4], "^" + test + "$")

    def test_failed_case_only_reports_owned_identifier(self):
        test = mod.CASES["group"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 1, "credential-marker phase=apply-create code=hermesacc-1234567890abcdef failure=application-create api_code=400 detail_code=123456 field=loginConfig cleanup=incomplete secret-marker", "secret-marker")])
        with self.assertRaises(RuntimeError) as caught:
            mod.run(self.env(), runner)
        self.assertIn("hermesacc-1234567890abcdef", str(caught.exception))
        self.assertIn("cleanup=incomplete", str(caught.exception))
        self.assertIn("failure=application-create", str(caught.exception))
        self.assertIn("api_code=400", str(caught.exception))
        self.assertIn("detail_code=123456", str(caught.exception))
        self.assertIn("field=loginConfig", str(caught.exception))
        self.assertNotIn("secret-marker", str(caught.exception))
        self.assertNotIn("credential-marker", str(caught.exception))

    def test_group_member_failure_keeps_safe_username_and_unknown_cleanup(self):
        test = "TestDestructiveLiveGroupMemberTrace"
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 1, "phase=plan-converged code=hermesacc-1234567890abcdef username=hermesacc-fedcba0987654321 cleanup=unknown secret-marker", "credential-marker")])
        with patch.dict(mod.CASES, {"group_member": test}), self.assertRaises(RuntimeError) as caught:
            mod.run(self.env("group_member"), runner)
        message = str(caught.exception)
        self.assertIn("plan-converged", message)
        self.assertIn("hermesacc-1234567890abcdef", message)
        self.assertIn("username=hermesacc-fedcba0987654321", message)
        self.assertIn("cleanup=unknown", message)
        self.assertNotIn("secret-marker", message)
        self.assertNotIn("credential-marker", message)

    def test_drift_reason_is_allowlisted(self):
        test = mod.CASES["application"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 1, "phase=remote-drift code=hermesacc-1234567890abcdef reason=update-rejected cleanup=confirmed secret-marker", "secret-marker")])
        with self.assertRaises(RuntimeError) as caught:
            mod.run(self.env("application"), runner)
        self.assertIn("reason=update-rejected", str(caught.exception))
        self.assertNotIn("secret-marker", str(caught.exception))

    def test_public_account_requires_full_stage_evidence(self):
        test = mod.CASES["public_account"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                  subprocess.CompletedProcess([], 0, "ok", "")])
        with self.assertRaisesRegex(RuntimeError, "stage evidence"):
            mod.run(self.env("public_account"), runner)

    def test_public_account_rejects_incomplete_duplicate_and_foreign_evidence(self):
        code = "hermesacc-1234567890abcdef"
        lines = [f"public-account phase={phase} code={code} result=passed" for phase in mod.PUBLIC_ACCOUNT_PHASES]
        complete = f"public-account phase=complete code={code} cleanup=confirmed state_id_sha256=" + "a" * 64
        invalid = []
        for index in range(len(lines)):
            invalid.append(lines[:index] + lines[index + 1:] + [complete])
        invalid += [lines + [lines[0], complete], [line.replace(code, "hermesacc-fedcba0987654321") if index == 2 else line for index, line in enumerate(lines)] + [complete], lines, lines + [complete.replace("confirmed", "unknown")]]
        test = mod.CASES["public_account"]
        for output in invalid:
            runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), subprocess.CompletedProcess([], 0, "\n".join(output), "")])
            with self.assertRaises(RuntimeError):
                mod.run(self.env("public_account"), runner)

    def test_public_account_actual_failures_survive_outer(self):
        root = Path(__file__).resolve().parents[2]
        for go_test, phase, cleanup, pinned in (
            ("TestMockPublicAccountUnknownCreateIDIsNeverDeleted", "apply-create", "unknown", False),
            ("TestMockPublicAccountReconcileAndCleanupFailures/reconcile", "apply-reconcile", "confirmed", True),
            ("TestMockPublicAccountReconcileAndCleanupFailures/cleanup", "apply-reconcile", "incomplete", True),
        ):
            result = subprocess.run(["go", "test", "-count=1", "-v", "-run", "^" + go_test + "$", "./scripts/acceptance"], cwd=root, capture_output=True, text=True, check=False)
            self.assertEqual(result.returncode, 0, "offline public-account failure injection failed")
            result.returncode = 1
            test = mod.CASES["public_account"]
            runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), result])
            with self.assertRaises(RuntimeError) as caught:
                mod.run(self.env("public_account"), runner)
            text = str(caught.exception)
            for value in (phase, "hermesacc-1234567890abcdef", "cleanup=" + cleanup):
                self.assertIn(value, text)
            self.assertEqual("state_id_sha256=" in text, pinned)
            for secret in ("mock-public-id", "credential-marker", "secret-marker", "mock-token", "key-marker"):
                self.assertNotIn(secret, text)

    def test_public_account_actual_success_survives_outer(self):
        root = Path(__file__).resolve().parents[2]
        result = subprocess.run(["go", "test", "-count=1", "-v", "-run", "^TestMockPublicAccountTerraformLifecycle$", "./scripts/acceptance"], cwd=root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, "offline public-account lifecycle failed")
        test = mod.CASES["public_account"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), result])
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            mod.run(self.env("public_account"), runner)
        self.assertIn("phase=verify-all-update", output.getvalue())
        self.assertIn("phase=plan-import-converged", output.getvalue())
        self.assertIn("cleanup=confirmed state_id_sha256=", output.getvalue())

    def test_ext_idp_requires_full_stage_evidence_on_success(self):
        test = mod.CASES["ext_idp"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                  subprocess.CompletedProcess([], 0, "ok", "")])
        with self.assertRaisesRegex(RuntimeError, "stage evidence"):
            mod.run(self.env("ext_idp"), runner)

    def test_ext_idp_first_failure_and_cleanup_survive_outer_allowlist(self):
        test = mod.CASES["ext_idp"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                  subprocess.CompletedProcess([], 1, "external-idp phase=apply-name-update code=hermesacc-1234567890abcdef cleanup=confirmed secret-marker", "credential-marker")])
        with self.assertRaises(RuntimeError) as caught:
            mod.run(self.env("ext_idp"), runner)
        for value in ("apply-name-update", "hermesacc-1234567890abcdef", "cleanup=confirmed"):
            self.assertIn(value, str(caught.exception))
        self.assertNotIn("credential-marker", str(caught.exception))

    def test_webhook_first_failure_survives_outer_allowlist(self):
        test = "TestDestructiveLiveWebhookTrace"
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 1, "webhook phase=apply-reconcile code=hermesacc-1234567890abcdef cleanup=confirmed secret-marker", "secret-marker")])
        with patch.dict(mod.CASES, {"webhook": test}), self.assertRaises(RuntimeError) as caught:
            mod.run(self.env("webhook"), runner)
        output = str(caught.exception)
        for expected in ("apply-reconcile", "hermesacc-1234567890abcdef", "cleanup=confirmed"):
            self.assertIn(expected, output)
        self.assertNotIn("secret-marker", output)

    def test_ext_idp_actual_failed_create_preserves_first_failure_through_outer(self):
        root = Path(__file__).resolve().parents[2]
        result = subprocess.run(["go", "test", "-count=1", "-v", "-run", "^TestMockExtIdpFailedCreateWithoutStateNeverDeletes$", "./scripts/acceptance"],
                                cwd=root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, "offline failure-safety test failed")
        # The mock safety assertion passes; replay its actual captured tracer failure
        # as a failing live invocation, without substituting any API response.
        result.returncode = 1
        test = mod.CASES["ext_idp"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), result])
        with self.assertRaises(RuntimeError) as caught:
            mod.run(self.env("ext_idp"), runner)
        for value in ("apply-create", "hermesacc-1234567890abcdef", "cleanup=unknown"):
            self.assertIn(value, str(caught.exception))
        for secret in ("credential-marker", "secret-marker", "mock-token", "key-marker"):
            self.assertNotIn(secret, str(caught.exception))

    def test_ext_idp_actual_mock_trace_survives_outer_success_allowlist(self):
        root = Path(__file__).resolve().parents[2]
        result = subprocess.run(["go", "test", "-count=1", "-v", "-run", "^TestMockExtIdpTerraformTrace$", "./scripts/acceptance"],
                                cwd=root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, "offline external IdP tracer failed")
        test = mod.CASES["ext_idp"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""), result])
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            mod.run(self.env("ext_idp"), runner)
        for phase in mod.EXT_IDP_PHASES:
            self.assertIn(f"phase={phase} code=hermesacc-1234567890abcdef result=passed", output.getvalue())
        self.assertIn("cleanup=confirmed", output.getvalue())
        self.assertIn("sandbox case ext_idp passed", output.getvalue())
        for secret in ("credential-marker", "secret-marker", "mock-token", "key-marker"):
            self.assertNotIn(secret, output.getvalue())


if __name__ == "__main__":
    unittest.main()
