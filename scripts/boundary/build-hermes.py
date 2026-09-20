#!/usr/bin/env python3
"""Build a qualification image without copying host native state or credentials."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

SOURCE_COMMIT = "6a627e6eb38e28ac421d5ad8df3f676e49d0c287"
ROOT = Path(__file__).resolve().parents[2]


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
    parser.add_argument("--source", type=Path, default=Path.home() / ".hermes/hermes-agent")
    args = parser.parse_args()
    source = args.source.expanduser().resolve(strict=True)
    actual = run(["git", "-C", str(source), "rev-parse", SOURCE_COMMIT + "^{commit}"], capture_output=True).stdout.strip()
    if actual != SOURCE_COMMIT:
        raise SystemExit("pinned Hermes source is unavailable")
    arch = run(["docker", "version", "--format", "{{.Server.Arch}}"], capture_output=True).stdout.strip()
    if arch not in ("amd64", "arm64"):
        raise SystemExit("unsupported Docker architecture")
    run(["make", "build-boundary"], cwd=ROOT, env={**os.environ, "GOARCH": arch})
    parent = ROOT / ".cache/boundary"
    parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    context = Path(tempfile.mkdtemp(prefix="hermes-", dir=parent))
    with (context / "hermes.tar").open("wb") as archive:
        subprocess.run(["git", "-C", str(source), "archive", "--format=tar", SOURCE_COMMIT], stdout=archive, check=True)
    shutil.copyfile(ROOT / "config/boundary/hermes.Dockerfile", context / "Dockerfile")
    for name in ("vigil-guardian", "vigil-worker"):
        shutil.copy2(ROOT / "bin" / name, context / name)
    tag = "vigil/hermes-worker:" + SOURCE_COMMIT[:12]
    run(["docker", "build", "--label", "vigil.qualification=stage5", "--label", "vigil.hermes-source=" + SOURCE_COMMIT, "--tag", tag, str(context)])
    image = run(["docker", "image", "inspect", "--format", "{{.Id}}", tag], capture_output=True).stdout.strip()
    manifest = {
        "schema_version": 1,
        "purpose": "unqualified-boundary-image",
        "harness": "hermes",
        "source_commit": SOURCE_COMMIT,
        "architecture": arch,
        "image_id": image,
        "image_tag": tag,
        "production_eligible": False,
        "inputs": {name: digest(context / name) for name in ("Dockerfile", "hermes.tar", "vigil-guardian", "vigil-worker")},
    }
    destination = context / "image.json"
    destination.write_text(json.dumps(manifest, indent=2) + "\n")
    destination.chmod(0o600)
    print(destination)


if __name__ == "__main__":
    main()
