#!/usr/bin/env python3
"""Build an architecture-specific Codex boundary image without credentials."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

VERSION = "0.155.1"
ROOT = Path(__file__).resolve().parents[2]
TARGETS = {
    "amd64": ("x64", "x86_64-unknown-linux-musl"),
    "arm64": ("arm64", "aarch64-unknown-linux-musl"),
}


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, text=True, **kwargs)


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--npm-root",
        type=Path,
        help="npm global module root containing the exact @openai/codex package",
    )
    parser.add_argument(
        "--platform-package",
        type=Path,
        help="extracted exact @openai/codex Linux architecture package",
    )
    args = parser.parse_args()
    arch = run(
        ["docker", "version", "--format", "{{.Server.Arch}}"], capture_output=True
    ).stdout.strip()
    if arch not in TARGETS:
        raise SystemExit("unsupported Docker architecture")
    package_arch, target = TARGETS[arch]
    if args.platform_package is not None:
        package = args.platform_package.expanduser().resolve(strict=True)
    else:
        npm_root = args.npm_root
        if npm_root is None:
            npm_root = Path(run(["npm", "root", "-g"], capture_output=True).stdout.strip())
        npm_root = npm_root.expanduser().resolve(strict=True)
        package = (
            npm_root
            / "@openai/codex/node_modules/@openai"
            / f"codex-linux-{package_arch}"
        )
    metadata = json.loads((package / "package.json").read_text())
    if metadata.get("version") != f"{VERSION}-linux-{package_arch}":
        raise SystemExit("installed Codex architecture package does not match the pin")
    vendor = package / "vendor" / target
    codex = (vendor / "bin/codex").resolve(strict=True)
    rg = (vendor / "codex-path/rg").resolve(strict=True)
    if platform.system() == "Linux":
        actual = run([str(codex), "--version"], capture_output=True).stdout.strip()
        if actual != f"codex-cli {VERSION}":
            raise SystemExit("installed Codex executable does not match the pin")

    run(["make", "build-boundary"], cwd=ROOT, env={**os.environ, "GOARCH": arch})
    parent = ROOT / ".cache/boundary"
    parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    context = Path(tempfile.mkdtemp(prefix="codex-", dir=parent))
    shutil.copyfile(ROOT / "config/boundary/codex.Dockerfile", context / "Dockerfile")
    shutil.copy2(codex, context / "codex")
    shutil.copy2(rg, context / "rg")
    for name in ("vigil-guardian", "vigil-worker"):
        shutil.copy2(ROOT / "bin" / name, context / name)

    tag = f"vigil/codex-worker:{VERSION}-{arch}"
    run(
        [
            "docker",
            "build",
            "--label",
            "vigil.qualification=stage5",
            "--label",
            f"vigil.codex-version={VERSION}",
            "--tag",
            tag,
            str(context),
        ]
    )
    image = run(
        ["docker", "image", "inspect", "--format", "{{.Id}}", tag],
        capture_output=True,
    ).stdout.strip()
    actual = run(
        [
            "docker",
            "run",
            "--rm",
            "--pull=never",
            "--network=none",
            "--read-only",
            "--cap-drop=ALL",
            "--entrypoint=/opt/codex/codex",
            image,
            "--version",
        ],
        capture_output=True,
    ).stdout.strip()
    if actual != f"codex-cli {VERSION}":
        raise SystemExit("built Codex executable does not match the pin")
    manifest = {
        "schema_version": 1,
        "purpose": "unqualified-boundary-image",
        "harness": "codex",
        "harness_version": VERSION,
        "architecture": arch,
        "image_id": image,
        "image_tag": tag,
        "credential_mode": "chatgpt_subscription_private_home",
        "credential_in_image": False,
        "model": "gpt-5.6-luna",
        "reasoning_effort": "low",
        "production_eligible": False,
        "inputs": {
            name: digest(context / name)
            for name in (
                "Dockerfile",
                "codex",
                "rg",
                "vigil-guardian",
                "vigil-worker",
            )
        },
    }
    destination = context / "image.json"
    destination.write_text(json.dumps(manifest, indent=2) + "\n")
    destination.chmod(0o600)
    print(destination)


if __name__ == "__main__":
    main()
