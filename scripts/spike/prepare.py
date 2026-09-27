#!/usr/bin/env python3
"""Reproduce Stage 1 preparation with the existing Hermes Python environment.

Development helper, not an application runtime or a transport adapter. No inference.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import tarfile
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
TEMPLATES = ROOT / "config/spike"


def read_json(path):
    return json.loads(path.read_text())


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")
    path.chmod(0o600)


def run(argv, *, cwd, env=None):
    return subprocess.run(argv, cwd=cwd, env=env, check=True,
                          capture_output=True, text=True, timeout=30).stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hermes-root", type=Path)
    parser.add_argument("--codex", default="codex")
    parser.add_argument("--harness", choices=("both", "hermes"), default="both")
    parser.add_argument("--base-url")
    parser.add_argument("--model")
    parser.add_argument("--fixture-source", type=Path,
                        help="Read-only Git repository whose HEAD populates the disposable fixture")
    parser.add_argument("--qualification-spec", type=Path,
                        help="Bounded Markdown copied into the disposable fixture before its baseline")
    args = parser.parse_args()
    profiles = read_json(TEMPLATES / "profiles.json")
    hermes = profiles["hermes"]
    installation = (args.hermes_root or Path(hermes["installation"]).expanduser()).resolve()
    interpreter = installation / hermes["python_relative"]
    codex = shutil.which(args.codex)
    if not interpreter.is_file() or (args.harness == "both" and not codex):
        raise ValueError("The selected pinned harness installations are required")
    if (installation / ".env").exists():
        raise ValueError("Hermes installation .env would be inherited; use a clean installation (do not delete the user's file)")
    hermes["base_url"] = args.base_url or hermes["base_url"]
    hermes["model"] = args.model or hermes["model"]
    url = urlsplit(hermes["base_url"])
    if (url.scheme != "http" or url.hostname not in {"127.0.0.1", "localhost", "::1"}
            or url.username or url.password or url.query or url.fragment or url.path != "/v1"):
        raise ValueError("This spike requires a credential-free loopback HTTP /v1 endpoint")
    if not hermes["model"].strip():
        raise ValueError("Explicit model required")
    version = run([codex, "--version"], cwd=ROOT) if args.harness == "both" else ""
    commit = run(["git", "rev-parse", "HEAD"], cwd=installation)
    if ((args.harness == "both" and version != "codex-cli " + profiles["codex"]["version"])
            or commit != hermes["source_commit"]):
        raise ValueError("Harness version changed; refresh source/protocol evidence before preparing")

    # A new private directory every time; never reuse/reset a previous experiment.
    os.umask(0o077)
    parent = ROOT / ".cache/spike"
    parent.mkdir(parents=True, exist_ok=True)
    work = Path(tempfile.mkdtemp(prefix="stage1-", dir=parent))
    fixture = work / "fixture"
    if args.fixture_source:
        source = args.fixture_source.expanduser().resolve()
        run(["git", "rev-parse", "--verify", "HEAD"], cwd=source)
        archive = subprocess.run(["git", "archive", "--format=tar", "HEAD"], cwd=source,
                                 check=True, capture_output=True, timeout=30).stdout
        fixture.mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive), mode="r:") as bundle:
            for member in bundle.getmembers():
                target = (fixture / member.name).resolve()
                if not target.is_relative_to(fixture.resolve()):
                    raise ValueError("fixture archive contains an escaping path")
            bundle.extractall(fixture, filter="data")
    else:
        shutil.copytree(ROOT / "testdata/spike", fixture)
    if args.qualification_spec:
        spec_source = args.qualification_spec.expanduser().resolve()
        if not spec_source.is_file() or spec_source.is_symlink() or spec_source.stat().st_size > 65536:
            raise ValueError("qualification spec must be one regular Markdown file of at most 64 KiB")
        shutil.copyfile(spec_source, fixture / "VIGIL-QUALIFICATION.md")
    native_home = work / "hermes-home"
    native_home.mkdir()
    user_home = work / "user-home"
    user_home.mkdir()
    codex_home = work / "codex-home"
    if args.harness == "both":
        codex_home.mkdir()
        shutil.copyfile(TEMPLATES / "codex.toml", codex_home / "config.toml")
    evidence = work / "evidence"
    evidence.mkdir()
    env = {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "HOME": str(user_home),
        "LANG": "C.UTF-8",
        "PYTHONDONTWRITEBYTECODE": "1",
        "PYTHONPATH": str(installation),
        "HERMES_HOME": str(native_home),
        "HERMES_TUI_TOOLSETS": ",".join(hermes["toolsets"]),
        "HERMES_TUI_CHECKPOINTS": "false",
        "HERMES_TUI_GATEWAY_SHUTDOWN_GRACE_S": "1",
        "SPIKE_WORKSPACE": str(fixture),
        "SPIKE_MODEL": hermes["model"],
        "SPIKE_BASE_URL": hermes["base_url"],
        # Public dummy for unauthenticated llama.cpp; real access is supplied at live launch only.
        "VIGIL_LLAMA_API_KEY": "local-no-auth",
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": os.devnull,
    }
    run(["git", "init", "-q", "-b", "spike"], cwd=fixture, env=env)
    run(["git", "add", "--all"], cwd=fixture, env=env)
    run(["git", "-c", "user.name=Adapter Spike", "-c", "user.email=spike@invalid",
         "-c", "commit.gpgsign=false", "commit", "-qm", "Disposable fixture baseline"], cwd=fixture, env=env)
    baseline = run(["git", "rev-parse", "HEAD"], cwd=fixture, env=env)
    config = read_json(TEMPLATES / hermes["config_template"])
    # Enumerate every auxiliary task from the PINNED installed defaults, including disabled tasks.
    aux_tasks = json.loads(run([str(interpreter), "-c",
        "import json; from hermes_cli.config_defaults import DEFAULT_CONFIG; "
        "print(json.dumps(sorted(k for k,v in DEFAULT_CONFIG['auxiliary'].items() if isinstance(v,dict))))"],
        cwd=fixture, env=env))
    for task in aux_tasks:
        config["auxiliary"].setdefault(task, {}).update({
            "provider": "custom", "model": "${SPIKE_MODEL}", "base_url": "${SPIKE_BASE_URL}",
            "api_key": "${VIGIL_LLAMA_API_KEY}", "fallback_chain": [], "timeout": 20,
        })
    # JSON is also valid YAML; the native Hermes loader reads this config.yaml.
    write_json(native_home / "config.yaml", config)
    verify_env = {**env, "SPIKE_EXPECTED": json.dumps(hermes)}
    try:
        run([str(interpreter), str(Path(__file__).with_name("verify_hermes.py")), str(evidence / "hermes-effective.json")],
                     cwd=fixture, env=verify_env)
    except subprocess.CalledProcessError as exc:
        # Private diagnostics only. Do not print potentially sensitive native stderr.
        (evidence / "verify.stdout").write_text(exc.stdout)
        (evidence / "verify.stderr").write_text(exc.stderr)
        raise ValueError(f"Hermes inspection failed; private diagnostics: {evidence}") from None
    effective = read_json(evidence / "hermes-effective.json")
    launch = {
        "schema_version": 1,
        "stage": "prepared-no-inference",
        "workspace": str(fixture), "fixture_baseline": baseline,
        "limits": profiles["limits"],
        "limits_status": "Stage 2 runner must enforce limits; native settings alone do not enforce them",
        "hermes": {"argv": [str(interpreter), "-m", "tui_gateway.entry"], "cwd": str(fixture),
                   "env": env, "secret_env_reference": hermes["api_key_env"],
                   "session_create": {"cwd": str(fixture), "model": hermes["model"],
                                      "provider": "custom", "close_on_disconnect": True}},
        "codex": {}
    }
    if args.harness == "both":
        launch["codex"] = {"argv": [codex, "app-server", "--stdio"], "cwd": str(fixture),
                           "env": {k: env[k] for k in ("PATH", "HOME", "LANG", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL")},
                           "auth_file_reference": str(Path(profiles["codex"]["auth_file_reference"]).expanduser()),
                           "thread_start": {"model": profiles["codex"]["model"], "modelProvider": "openai",
                                            "cwd": str(fixture), "approvalPolicy": "on-request", "approvalsReviewer": "user",
                                            "sandbox": "workspace-write", "ephemeral": False}}
        launch["codex"]["env"]["CODEX_HOME"] = str(codex_home)
        features = run([codex, "features", "list"], cwd=fixture, env=launch["codex"]["env"])
        if not any(line.startswith("multi_agent") and line.rstrip().endswith("false") for line in features.splitlines()):
            raise ValueError("Codex effective multi_agent setting is not disabled")
    write_json(work / "launch.json", launch)
    source_files = ["hermes_cli/config_defaults.py", "hermes_cli/env_loader.py", "hermes_cli/fallback_config.py",
                    "agent/auxiliary_client.py", "tui_gateway/server.py", "tui_gateway/session_auto_continue.py",
                    "tui_gateway/agent_callbacks.py", "tui_gateway/methods_session.py",
                    "tui_gateway/session_lifecycle.py", "tui_gateway/prompt_turn.py",
                    "tui_gateway/server_requests.py", "tui_gateway/contracts/sessions.py",
                    "tui_gateway/contracts/server_requests.py", "tools/approval.py",
                    "tools/terminal_tool.py", "toolsets.py"]
    report = {"codex_version": version, "hermes_commit": commit,
              "model_turns_started": 0, "codex_config_parse_passed": args.harness == "both",
              "codex_multi_agent_disabled": args.harness == "both", "effective_checks": effective,
              "source_sha256": {f: hashlib.sha256((installation / f).read_bytes()).hexdigest() for f in source_files}}
    report["manifest_sha256"] = hashlib.sha256((work / "launch.json").read_bytes()).hexdigest()
    report["config_sha256"] = {
        "hermes-home/config.yaml": hashlib.sha256((native_home / "config.yaml").read_bytes()).hexdigest(),
    }
    if args.harness == "both":
        report["config_sha256"]["codex-home/config.toml"] = hashlib.sha256((codex_home / "config.toml").read_bytes()).hexdigest()
    write_json(evidence / "preparation.json", report)
    print(f"Prepared: {work}\nHermes effective settings verified; no inference started.\n"
          "Codex credentials are only referenced when --harness=both. See docs/history/adapter-spike.md before live launch.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f"Preparation failed: {exc}", file=sys.stderr)
        sys.exit(1)
