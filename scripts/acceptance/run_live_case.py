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
    "application_strategy": "TestDestructiveLiveApplicationStrategyTrace",
    "namespace": "TestDestructiveLiveNamespaceTrace",
    "user": "TestDestructiveLiveUserTrace",
    "public_account": "TestDestructiveLivePublicAccountTrace",
    "ext_idp": "TestDestructiveLiveExtIdpTrace",
    "webhook": "TestDestructiveLiveWebhookTrace",
    "organization": "TestDestructiveLiveOrganizationTrace",
    "role": "TestDestructiveLiveRoleTrace",
    "resource": "TestDestructiveLiveResourceTrace",
    "post": "TestDestructiveLivePostTrace",
    "data_resource": "TestDestructiveLiveDataResourceTrace",
    "data_object": "TestDestructiveLiveObjectTrace",
    "department": "TestDestructiveLiveDepartmentTrace",
    "role_assignment": "TestDestructiveLiveRoleAssignmentTrace",
    "department_member": "TestDestructiveLiveDepartmentMemberTrace",
    "pipeline_function": "TestDestructiveLivePipelineFunctionTrace",
    "data_object_field": "TestDestructiveLiveDataObjectFieldReplacement",
}

# Tracers print generated non-secret identifiers for possible manual cleanup.
SAFE_DIAGNOSTIC = re.compile(
    r"\bphase=([a-z][a-z0-9-]{0,50})\s+(?:code|username|name)=(hermesacc-[0-9a-f]{16})(?:\s|$)"
)
SAFE_USERNAME = re.compile(r"\busername=(hermesacc-[0-9a-f]{16})(?:\s|$)")
GROUP_PHASES = ('preflight-absent', 'apply-create', 'capture-id', 'verify-created', 'plan-converged', 'configure-all-update', 'plan-all-update', 'apply-all-update', 'verify-all-update', 'plan-update-converged', 'configure-original', 'apply-original', 'verify-original', 'configure-empty-description', 'apply-empty-description', 'verify-empty-description', 'restore-description', 'plan-code-replacement', 'plan-type-replacement', 'import-fresh-state', 'verify-imported-id', 'plan-import-converged', 'remote-drift', 'plan-drift', 'apply-reconcile', 'plan-reconverged', 'verify-owned-before-destroy', 'destroy', 'verify-absent')
GROUP_PASSED = re.compile(r"^group phase=([a-z-]+) code=(hermesacc-[0-9a-f]{16}) result=passed$", re.MULTILINE)
PUBLIC_ACCOUNT_PHASES = (
    "preflight-absent", "apply-create", "capture-id", "verify-created", "plan-converged",
    "configure-all-update", "plan-all-update", "apply-all-update", "verify-all-update",
    "plan-update-converged", "configure-original", "apply-original", "verify-original",
    "import-fresh-state", "verify-imported-id", "plan-import-converged", "remote-drift",
    "plan-drift", "apply-reconcile", "plan-reconverged", "verify-owned-before-destroy",
    "destroy", "verify-absent",
)
PUBLIC_ACCOUNT_PASSED = re.compile(
    r"^public-account phase=([a-z-]+) code=(hermesacc-[0-9a-f]{16}) result=passed$", re.MULTILINE
)
EXT_IDP_PHASES = (
    "apply-create", "verify-created-id", "plan-converged",
    "configure-name-update", "plan-name-update", "apply-name-update",
    "verify-name-update", "plan-name-converged", "configure-original-name",
    "apply-original-name", "verify-original-name", "import-fresh-state",
    "verify-imported-id", "plan-import-converged", "remote-drift", "plan-drift",
    "apply-reconcile", "plan-reconverged", "verify-owned-before-destroy",
    "destroy", "verify-absent",
)
EXT_IDP_PASSED = re.compile(
    r"^external-idp phase=([a-z-]+) code=(hermesacc-[0-9a-f]{16}) result=passed$", re.MULTILINE
)
SAFE_FAILURE = re.compile(
    r"\bfailure=(application-create|permission-strategy-update|permission-strategy-readback|permission-strategy-mismatch|inconsistent-result|unclassified)\b"
)
SAFE_API_CODE = re.compile(r"\bapi_code=([0-9]{3,6})\b")
SAFE_DETAIL_CODE = re.compile(r"\bdetail_code=([0-9]{1,10})\b")
SAFE_FIELD = re.compile(
    r"\bfield=(appIdentifier|appName|appType|appDescription|defaultProtocol|redirectUris|logoutRedirectUris|ssoEnabled|oidcConfig|samlConfig|oauthConfig|casConfig|loginConfig|registerConfig|brandingConfig|multiple)\b"
)
SAFE_DRIFT_REASON = re.compile(
    r"\breason=(ownership-unverified|update-transport|update-invalid|update-rejected|update-unsuccessful|readback-unavailable|readback-name-mismatch|readback-marker-mismatch|unknown)\b"
)
SAFE_TENANT_STAGE = re.compile(
    r"\bstage=(?:list|get|apps|users|admins|organizations|guard) "
    r"category=(?:unknown|not-run|guard|transport|invalid-envelope|http-other|http-4xx|http-5xx|business-other|business-4xx|business-5xx|complete|candidate|absent|empty|nonempty|foreign|duplicate|invalid-shape|incomplete-inventory|page-cap) "
    r"http_error=[0-9]{1,3} business=[0-9]{1,3} api_code=[0-9]{1,10} "
    r"shape=(?:unavailable|count-list|identity|appids|invalid-data|missing-or-invalid-count|missing-or-invalid-list|invalid-item|invalid-identity|missing-or-invalid-appids) "
    r"pages=[0-9]{1,3} count=-?[0-9]{1,10} matches=[0-9]{1,10} total=-?[0-9]{1,10}(?=\s|$)"
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
    result = runner(["go", "test", "-count=1", "-run", "^" + test + "$", "./scripts/acceptance"] +
                    (["-v"] if case in ("group", "ext_idp", "public_account") else []) + [
                     "-args", "-authing-destructive-sandbox"],
                    cwd=repo, env=env, capture_output=True, text=True, check=False)
    if result.returncode:
        # Provider and API bodies, stdout and stderr are untrusted; never print them.
        matches = SAFE_DIAGNOSTIC.findall(result.stdout + "\n" + result.stderr)
        if matches:
            phase, identifier = matches[-1]
            controlled = result.stdout + "\n" + result.stderr
            cleanup = re.findall(r"\bcleanup=(confirmed|incomplete|unknown)\b", controlled)
            outcome = f"; cleanup={cleanup[-1]}" if cleanup else ""
            if case == "group_member":
                usernames = SAFE_USERNAME.findall(controlled)
                if usernames:
                    outcome += f"; username={usernames[-1]}"
            failure = SAFE_FAILURE.findall(controlled)
            api_code = SAFE_API_CODE.findall(controlled)
            detail_code = SAFE_DETAIL_CODE.findall(controlled)
            field = SAFE_FIELD.findall(controlled)
            reason = SAFE_DRIFT_REASON.findall(controlled)
            if failure:
                outcome += f"; failure={failure[-1]}"
                if api_code:
                    outcome += f"; api_code={api_code[-1]}"
                if detail_code:
                    outcome += f"; detail_code={detail_code[-1]}"
                if field:
                    outcome += f"; field={field[-1]}"
            if phase == "remote-drift" and reason:
                outcome += f"; reason={reason[-1]}"
            if case == "group":
                diagnostic = re.search(r"\bgroup_readback=(?:identity|foreign|absent|http-error|business-error|invalid) description_readback=(?:empty|string|null|missing|unknown) business=[0-9]{1,3} api_code=[0-9]{1,10}(?=\s|$)", controlled)
                if diagnostic:
                    outcome += "; " + diagnostic.group(0)
            if case == "tenant":
                diagnostic = SAFE_TENANT_STAGE.search(controlled)
                if diagnostic:
                    outcome += "; " + diagnostic.group(0)
            if case in ("group", "tenant", "public_account"):
                pinned = re.search(r"\bstate_id_sha256=([0-9a-f]{64})(?=\s|$)", controlled)
                if pinned:
                    outcome += "; state_id_sha256=" + pinned.group(1)
            raise RuntimeError(f"sandbox case {case} failed at {phase}; inspect only owned {identifier}{outcome}")
        raise RuntimeError(f"sandbox case {case} failed (output suppressed)")
    if case == "group":
        evidence = GROUP_PASSED.findall(result.stdout)
        codes = {code for _, code in evidence}
        if tuple(phase for phase, _ in evidence) != GROUP_PHASES or len(codes) != 1:
            raise RuntimeError("group stage evidence missing or inconsistent (output suppressed)")
        code = codes.pop()
        complete = re.search(r"^group phase=complete code=" + re.escape(code) +
                             r" cleanup=confirmed state_id_sha256=([0-9a-f]{64})$", result.stdout, re.MULTILINE)
        if not complete:
            raise RuntimeError("group cleanup evidence missing (output suppressed)")
        for phase in GROUP_PHASES:
            print(f"group phase={phase} code={code} result=passed")
        print(f"group code={code} cleanup=confirmed state_id_sha256={complete.group(1)}")
    if case == "public_account":
        evidence = PUBLIC_ACCOUNT_PASSED.findall(result.stdout)
        codes = {code for _, code in evidence}
        if tuple(phase for phase, _ in evidence) != PUBLIC_ACCOUNT_PHASES or len(codes) != 1:
            raise RuntimeError("public-account stage evidence missing or inconsistent (output suppressed)")
        code = codes.pop()
        complete = re.search(r"^public-account phase=complete code=" + re.escape(code) +
                             r" cleanup=confirmed state_id_sha256=([0-9a-f]{64})$", result.stdout, re.MULTILINE)
        if not complete:
            raise RuntimeError("public-account cleanup evidence missing (output suppressed)")
        for phase in PUBLIC_ACCOUNT_PHASES:
            print(f"public_account phase={phase} code={code} result=passed")
        print(f"public_account code={code} cleanup=confirmed state_id_sha256={complete.group(1)}")
    if case == "ext_idp":
        evidence = EXT_IDP_PASSED.findall(result.stdout)
        codes = {code for _, code in evidence}
        if tuple(phase for phase, _ in evidence) != EXT_IDP_PHASES or len(codes) != 1:
            raise RuntimeError("external IdP stage evidence missing or inconsistent (output suppressed)")
        code = codes.pop()
        if f"external-idp phase=complete code={code} cleanup=confirmed" not in result.stdout.splitlines():
            raise RuntimeError("external IdP cleanup evidence missing (output suppressed)")
        for phase in EXT_IDP_PHASES:
            print(f"ext_idp phase={phase} code={code} result=passed")
        print(f"ext_idp code={code} cleanup=confirmed")
    print(f"sandbox case {case} passed")


if __name__ == "__main__":
    try:
        run()
    except (ValueError, RuntimeError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
