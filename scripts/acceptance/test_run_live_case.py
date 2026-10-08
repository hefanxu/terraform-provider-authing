import re
import subprocess
import unittest
from pathlib import Path
from unittest.mock import Mock

import run_live_case as mod


class SandboxDispatchTests(unittest.TestCase):
    def test_workflow_choices_map_to_existing_live_tests(self):
        root = Path(__file__).resolve().parents[2]
        workflow = (root / ".github/workflows/authing-acceptance.yml").read_text()
        options = workflow.split("        options:\n", 1)[1].split("\npermissions:", 1)[0]
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

    def test_success_invokes_exact_test_and_flag(self):
        test = mod.CASES["group"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 0, "ok", "")])
        mod.run(self.env(), runner)
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

    def test_drift_reason_is_allowlisted(self):
        test = mod.CASES["application"]
        runner = Mock(side_effect=[subprocess.CompletedProcess([], 0, test + "\n", ""),
                                   subprocess.CompletedProcess([], 1, "phase=remote-drift code=hermesacc-1234567890abcdef reason=update-rejected cleanup=confirmed secret-marker", "secret-marker")])
        with self.assertRaises(RuntimeError) as caught:
            mod.run(self.env("application"), runner)
        self.assertIn("reason=update-rejected", str(caught.exception))
        self.assertNotIn("secret-marker", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
