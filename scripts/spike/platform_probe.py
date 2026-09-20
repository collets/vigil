#!/usr/bin/env python3
"""Disposable POSIX identity/lock and macOS sandbox primitives; no inference.

This is a qualification prototype, not production ownership or containment code.
"""
import argparse
import fcntl
import json
import os
from pathlib import Path
import platform
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import unicodedata

ROOT = Path(__file__).resolve().parents[2]


def lock_available(path):
    with path.open("a") as stream:
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return False
        fcntl.flock(stream, fcntl.LOCK_UN)
        return True


def overlaps(left, right):
    """Prototype for existing directory trees; identities, not spelling, decide."""
    left, right = left.resolve(strict=True), right.resolve(strict=True)
    return (any(left.samefile(parent) for parent in (right, *right.parents))
            or any(right.samefile(parent) for parent in (left, *left.parents)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hold", type=Path)
    args = parser.parse_args()
    if args.hold:
        with args.hold.open("a") as stream:
            fcntl.flock(stream, fcntl.LOCK_EX)
            print("locked", flush=True)
            signal.pause()
        return
    os.umask(0o077)
    (ROOT / ".cache").mkdir(exist_ok=True)
    base = Path(tempfile.mkdtemp(prefix="platform probe ", dir=ROOT / ".cache"))
    evidence = {"platform": platform.platform(), "machine": platform.machine(),
                "class": "disposable native primitives; not production coordinator/containment"}
    case = base / "CaseAlias"
    case.mkdir()
    lower = base / "casealias"
    composed = base / "caf\u00e9"
    composed.mkdir()
    decomposed = base / unicodedata.normalize("NFD", composed.name)
    alias = base / "symlink"
    alias.symlink_to(case, target_is_directory=True)
    nested = case / "nested"
    nested.mkdir()
    sibling = base / "CaseAlias-sibling"
    sibling.mkdir()
    evidence["path_identity"] = {
        "case_alias_same_inode": lower.exists() and lower.samefile(case),
        "unicode_alias_same_inode": decomposed.exists() and decomposed.samefile(composed),
        "symlink_same_inode": alias.samefile(case),
        "symlink_resolves": alias.resolve() == case.resolve(),
        "case_resolve_strings_equal": lower.resolve() == case.resolve(),
        "unicode_resolve_strings_equal": decomposed.resolve() == composed.resolve(),
        "space_in_test_path": " " in str(base),
        "ancestor_overlap_detected": overlaps(case, nested),
        "symlink_ancestor_overlap_detected": overlaps(alias, nested),
        "similar_prefix_sibling_not_overlap": not overlaps(case, sibling),
        "case_alias_overlap_detected": overlaps(lower, nested) if lower.exists() else None,
        "unicode_alias_overlap_detected": overlaps(composed, decomposed) if decomposed.exists() else None,
    }
    repo, linked = base / "repository", base / "linked worktree"
    env = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
    def git(*cmd, cwd=base):
        return subprocess.run(["git", *cmd], cwd=cwd, env=env, capture_output=True,
                              text=True, check=True, timeout=15).stdout.strip()
    git("init", "-q", "-b", "probe", str(repo))
    git("-c", "user.name=Fixture", "-c", "user.email=fixture@invalid",
        "-c", "commit.gpgsign=false", "commit", "-qm", "fixture", "--allow-empty", cwd=repo)
    git("worktree", "add", "-q", "-b", "linked", str(linked), cwd=repo)
    # Older supported Git versions lack --path-format; resolve relative output
    # against the repository instead of assuming it is already absolute.
    common = lambda path: (path / git("rev-parse", "--git-common-dir", cwd=path)).resolve()
    evidence["path_identity"]["linked_worktrees_share_git_identity"] = common(repo).samefile(common(linked))

    lock = case / "owner.lock"
    child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--hold", str(lock)],
                             stdout=subprocess.PIPE, text=True)
    try:
        assert child.stdout.readline().strip() == "locked"
        db_path = base / "quarantine-prototype.sqlite"
        with sqlite3.connect(db_path) as db:
            db.execute("CREATE TABLE claims (resource TEXT PRIMARY KEY, state TEXT NOT NULL)")
            db.execute("INSERT INTO claims VALUES ('fixture-workspace', 'unknown-writer')")
        held = not lock_available(lock)
        alias_held = not lock_available(alias / "owner.lock")
        os.kill(child.pid, signal.SIGKILL)
        child.wait(timeout=5)
        available = lock_available(lock)
        with sqlite3.connect(db_path) as db:
            state = db.execute("SELECT state FROM claims").fetchone()[0]
        evidence["ownership_prototype"] = {
            "concurrent_owner_blocked": held, "symlink_owner_blocked": alias_held,
            "os_lock_released_after_owner_death": available,
            "persistent_claim_after_owner_death": state,
            "dispatch_blocked_by_persistent_claim": state != "released",
            "production_coordinator_implemented": False,
        }
        assert held and alias_held and available and state == "unknown-writer"
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=5)

    sandbox = Path("/usr/bin/sandbox-exec")
    if platform.system() == "Darwin" and sandbox.exists():
        secret = base / "synthetic-secret.txt"
        secret.write_text("public fixture marker; not a real credential\n")
        profile = base / "probe.sb"
        quote = json.dumps
        profile.write_text('(version 1)\n(allow default)\n'
                           f'(deny file-read-data (literal {quote(str(secret))}))\n'
                           f'(deny file-write* (subpath {quote(str(repo / ".git"))}))\n'
                           '(deny network*)\n')
        server = socket.socket()
        server.bind(("127.0.0.1", 0))
        server.listen()
        try:
            cases = [
                ("allowed_worktree_write", "from pathlib import Path;Path('allowed.txt').write_text('fixture')"),
                ("protected_git_write", "from pathlib import Path;Path('.git/forbidden.txt').write_text('fixture')"),
                ("synthetic_secret_read", "from pathlib import Path;Path(" + repr(str(secret)) + ").read_text()"),
                ("loopback_network", "import socket;socket.create_connection(('127.0.0.1'," + str(server.getsockname()[1]) + "),2).close()"),
            ]
            outcomes = {}
            for name, source in cases:
                result = subprocess.run([str(sandbox), "-f", str(profile), sys.executable, "-c", source],
                                        cwd=repo, capture_output=True, timeout=10)
                outcomes[name] = result.returncode
            evidence["seatbelt_primitive"] = {"exit_codes": outcomes,
                "allowed_file_created": (repo / "allowed.txt").exists(),
                "protected_git_file_created": (repo / ".git/forbidden.txt").exists(),
                "profile": "allow-default with three explicit deny rules; NOT a complete worker policy",
                "production_containment_qualified": False}
        finally:
            server.close()
    destination = base / "platform-probe.json"
    destination.write_text(json.dumps(evidence, indent=2) + "\n")
    print(destination)


if __name__ == "__main__":
    main()
