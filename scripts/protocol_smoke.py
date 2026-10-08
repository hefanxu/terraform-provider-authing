#!/usr/bin/env python3
"""Exercise the built provider via Terraform's real plugin protocol, without API calls."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import zipfile

VERSION = "1.13.5"
ARCHIVE = f"terraform_{VERSION}_linux_amd64.zip"
# From https://releases.hashicorp.com/terraform/1.13.5/terraform_1.13.5_SHA256SUMS
SHA256 = "0dbe3fcc268eb670801af6a6456799d1ae26e72e73797f6c6167e18aafd1fd9a"
SOURCE = "registry.opentofu.org/authing/authing"
REPO = Path(__file__).resolve().parent.parent
TOOLS = REPO.parent / ".tools" / "terraform" / VERSION


def run(args, *, cwd=None, env=None):
    result = subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True, check=False)
    if result.returncode:
        raise RuntimeError(f"{' '.join(map(str, args))} failed ({result.returncode}):\n{result.stdout}\n{result.stderr}")
    return result


def install_terraform():
    TOOLS.mkdir(parents=True, exist_ok=True)
    manifest = TOOLS / f"terraform_{VERSION}_SHA256SUMS"
    archive = TOOLS / ARCHIVE
    base = f"https://releases.hashicorp.com/terraform/{VERSION}"
    if not manifest.exists():
        run(["curl", "-fsSL", "--retry", "3", "-o", str(manifest), f"{base}/{manifest.name}"])
    lines = [line.split() for line in manifest.read_text().splitlines()]
    matches = [digest for digest, name in lines if name == ARCHIVE]
    if matches != [SHA256]:
        raise RuntimeError(f"Official manifest checksum for {ARCHIVE} does not match pinned checksum")
    if not archive.exists():
        run(["curl", "-fsSL", "--retry", "3", "-o", str(archive), f"{base}/{ARCHIVE}"])
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    if digest != SHA256:
        raise RuntimeError(f"Archive checksum mismatch: expected {SHA256}, got {digest}")
    binary = TOOLS / "terraform"
    with zipfile.ZipFile(archive) as zf:
        binary.write_bytes(zf.read("terraform"))
    binary.chmod(0o755)
    print(run([str(binary), "version", "-no-color"]).stdout.strip())
    return binary


def check_attribute(schema, section, name, attribute, **flags):
    attrs = schema[section][name]["block"]["attributes"]
    actual = attrs[attribute]
    for flag, expected in flags.items():
        if actual.get(flag, False) != expected:
            raise AssertionError(f"{section}.{name}.{attribute}.{flag}: expected {expected}, got {actual.get(flag)}")


def main():
    terraform = install_terraform()
    with tempfile.TemporaryDirectory(prefix="protocol-smoke-", dir=TOOLS) as temp:
        root = Path(temp)
        provider_bin = root / "provider"
        provider_bin.mkdir()
        # Terraform's dev override locates the executable by terraform-provider-<name>.
        run(["go", "build", "-o", str(provider_bin / "terraform-provider-authing"), "."], cwd=REPO)
        config = root / "terraform.rc"
        config.write_text(f'provider_installation {{\n  dev_overrides {{\n    "{SOURCE}" = "{provider_bin}"\n  }}\n  direct {{}}\n}}\n')
        example = root / "example"
        example.mkdir()
        hcl = example / "main.tf"
        hcl.write_text(f'''terraform {{
  required_providers {{
    authing = {{
      source = "{SOURCE}"
    }}
  }}
}}

provider "authing" {{}}

resource "authing_user" "protocol_only" {{
  username = "not-created"
}}
''')
        env = {k: v for k, v in os.environ.items() if not k.startswith(("AUTHING_", "TF_"))}
        env.update({"TF_CLI_CONFIG_FILE": str(config), "TF_DATA_DIR": str(root / "data"),
                    "TF_INPUT": "0", "TF_IN_AUTOMATION": "1", "HOME": str(root),
                    "CHECKPOINT_DISABLE": "1"})
        validate = run([str(terraform), "validate", "-no-color"], cwd=example, env=env)
        print("terraform validate:", validate.stdout.strip())
        valid_hcl = hcl.read_text()
        hcl.write_text(valid_hcl.replace('username = "not-created"', 'unknown_smoke_attribute = "invalid"'))
        rejected = subprocess.run([str(terraform), "validate", "-no-color"], cwd=example,
                                  env=env, text=True, capture_output=True, check=False)
        if rejected.returncode == 0 or "Unsupported argument" not in (rejected.stdout + rejected.stderr):
            raise AssertionError(f"Terraform did not reject unknown provider attribute: {rejected.stdout} {rejected.stderr}")
        print("terraform validate (invalid attribute): rejected with Unsupported argument")
        hcl.write_text(valid_hcl)
        result = run([str(terraform), "providers", "schema", "-json"], cwd=example, env=env)
        schema = json.loads(result.stdout)["provider_schemas"]
        provider = schema[SOURCE]
        resources = provider["resource_schemas"]
        sources = provider["data_source_schemas"]
        assert len(resources) == 28, f"expected 28 resources, got {len(resources)}"
        assert len(sources) == 18, f"expected 18 data sources, got {len(sources)}"
        attrs = provider["provider"]["block"]["attributes"]
        assert attrs["access_key_secret"]["sensitive"] is True
        assert attrs["access_key_id"]["optional"] is True
        assert attrs["tenant_id"]["optional"] is True
        check_attribute(provider, "resource_schemas", "authing_user", "id", computed=True)
        check_attribute(provider, "resource_schemas", "authing_user", "password", sensitive=True)
        check_attribute(provider, "resource_schemas", "authing_user", "username", optional=True)
        check_attribute(provider, "data_source_schemas", "authing_user", "user_id", required=True)
        print(f"terraform providers schema -json: {len(resources)} resources, {len(sources)} data sources; representative attributes OK")
        print("No init, plan or apply executed; no Authing credentials supplied.")


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, RuntimeError, KeyError, ValueError, zipfile.BadZipFile) as exc:
        print(f"protocol smoke FAILED: {exc}", file=sys.stderr)
        sys.exit(1)
