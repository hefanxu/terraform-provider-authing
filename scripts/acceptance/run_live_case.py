#!/usr/bin/env python3
"""Execute one explicitly approved Authing sandbox lifecycle case without leaking output."""

import os
import re
import subprocess
import sys
from pathlib import Path

# Add an entry only after an offline Terraform CLI + httptest lifecycle passes.
CASES = {
    "group": "TestDestructiveLiveGroupTrace",
    "application": "TestDestructiveLiveApplicationTrace",
    "namespace": "TestDestructiveLiveNamespaceTrace",
    "user": "TestDestructiveLiveUserTrace",
    "public_account": "TestDestructiveLivePublicAccountTrace",
    "ext_idp": "TestDestructiveLiveExtIdpTrace",
    "webhook": "TestDestructiveLiveWebhookTrace",
    "organization": "TestDestructiveLiveOrganizationTrace",
    "invitation_policy": "TestDestructiveLiveInvitationPolicyTrace",
    "role": "TestDestructiveLiveRoleTrace",
    "resource": "TestDestructiveLiveResourceTrace",
    "post": "TestDestructiveLivePostTrace",
    "invitation_roster": "TestDestructiveLiveInvitationRosterTrace",
    "data_resource": "TestDestructiveLiveDataResourceTrace",
}

# Tracers print generated non-secret identifiers for possible manual cleanup.
SAFE_DIAGNOSTIC = re.compile(
    r"\bphase=([a-z][a-z0-9-]{0,50})\s+(?:code|username|name)=(hermesacc-[0-9a-f]{16})(?:\s|$)"
)


def run(env=None, runner=subprocess.run):
    env = os.environ if env is None else env
    case = env.get("ACCEPTANCE_TEST_CASE", "")
    if case not in CASES or env.get("AUTHING_ACCEPTANCE_CONFIRM") != "DESTRUCTIVE_SANDBOX":
        raise ValueError("unapproved or unsupported sandbox case")
    if not env.get("AUTHING_ACCESS_KEY_ID") or not env.get("AUTHING_ACCESS_KEY_SECRET"):
        raise ValueError("sandbox credentials are not configured")
    test = CASES[case]
    repo = Path(__file__).resolve().parents[2]
    listed = runner(["go", "test", "-list", "^" + test + "$", "./scripts/acceptance"],
                    cwd=repo, env=env, capture_output=True, text=True, check=False)
    if listed.returncode or test not in listed.stdout.splitlines():
        raise RuntimeError("selected live test is missing")
    result = runner(["go", "test", "-count=1", "-run", "^" + test + "$", "./scripts/acceptance",
                     "-args", "-authing-destructive-sandbox"],
                    cwd=repo, env=env, capture_output=True, text=True, check=False)
    if result.returncode:
        # Provider and API bodies, stdout and stderr are untrusted; never print them.
        matches = SAFE_DIAGNOSTIC.findall(result.stdout + "\n" + result.stderr)
        if matches:
            phase, identifier = matches[-1]
            raise RuntimeError(f"sandbox case {case} failed at {phase}; inspect only owned {identifier}")
        raise RuntimeError(f"sandbox case {case} failed (output suppressed)")
    print(f"sandbox case {case} passed")


if __name__ == "__main__":
    try:
        run()
    except (ValueError, RuntimeError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
