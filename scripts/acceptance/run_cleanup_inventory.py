#!/usr/bin/env python3
"""Read-only inventory runner: emit only structured closed diagnostics, never API output."""
import json
import os
import re
import subprocess
import sys
from pathlib import Path

FAMILIES=set('group user public_account application namespace ext_idp webhook pipeline_function tenant organization audit_tenant audit_org group_members group_member user_groups tenant_apps tenant_users tenant_admins tenant_organizations organization_root organization_children organization_members auth_flow_function'.split())
KINDS=set('candidate exact dependency relation page family known_exact recovered_exact gap'.split())
STATES=set('guard transport-or-http-error invalid-envelope absent business-error invalid-data invalid-page inventory-unavailable incomplete-inventory duplicate-or-missing-identity page-cap complete observed-not-authority discovered-not-verified identity-mismatch candidate-exact-identity candidate-not-authority invalid-appids scoped-candidate foreign-scope missing-root no-published-enumeration-or-exact-id exact-id-not-recovered'.split())
ALLOWED=set('kind family scope_sha256 generated_code id_sha256 state authority marker_matches business page count total'.split())

def safe_event(row):
    if not isinstance(row,dict) or set(row)-ALLOWED or row.get('kind') not in KINDS or row.get('family') not in FAMILIES or row.get('state') not in STATES:
        raise ValueError('invalid closed cleanup event')
    for key,value in row.items():
        if key in ('scope_sha256','id_sha256') and not re.fullmatch('[0-9a-f]{64}',str(value)):
            raise ValueError('invalid fingerprint')
        if key=='generated_code' and not re.fullmatch('hermesacc-[0-9a-f]{16}',str(value)):
            raise ValueError('invalid generated code')
        if key=='authority' and value!='none':
            raise ValueError('discovery cannot authorize deletion')
        if key=='marker_matches' and not isinstance(value,bool):
            raise ValueError('invalid marker diagnostic')
        if key in ('business','page','count','total') and (type(value) is not int or not -1<=value<=1000000):
            raise ValueError('invalid numeric diagnostic')
    return row

def run(env=None,runner=subprocess.run):
    env=os.environ if env is None else env
    if env.get('AUTHING_ACCEPTANCE_CONFIRM')!='READ_ONLY_SANDBOX' or not all(env.get(k) for k in ('AUTHING_ACCESS_KEY_ID','AUTHING_ACCESS_KEY_SECRET')):
        raise ValueError('read-only cleanup guard rejected input')
    root=Path(__file__).resolve().parents[2]
    test='TestReadOnlyCleanupInventory'
    result=runner(['go','test','-count=1','-v','-run','^'+test+'$','./scripts/acceptance','-args','-authing-cleanup-inventory-sandbox'],cwd=root,env=env,text=True,capture_output=True,check=False)
    rows=[]
    for line in result.stdout.splitlines():
        if line.startswith('CLEANUP_JSON '):rows.append(safe_event(json.loads(line.removeprefix('CLEANUP_JSON '))))
    if result.returncode or not rows or not any(r['kind']=='gap' for r in rows):
        raise RuntimeError('cleanup inventory execution incomplete (output suppressed)')
    for row in rows:print('CLEANUP_JSON '+json.dumps(row,sort_keys=True))

if __name__=='__main__':
    try:run()
    except (ValueError,RuntimeError,json.JSONDecodeError) as exc:
        print(str(exc),file=sys.stderr)
        sys.exit(1)
