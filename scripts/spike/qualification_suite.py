#!/usr/bin/env python3
"""Bounded platform qualification with fresh fixtures and process ancestry evidence.

Explicitly opt in to at most 16 model turns. No retries or automatic replay.
Process evidence contains executable names, never command arguments/environment.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = {"edit", "resume", "interrupt", "child", "loss", "clarify",
             "approval-allow", "approval-deny"}


def ancestry(root_pid, fixture, known):
    writer = fixture / "writer.pid"
    if writer.exists():
        try:
            known.add(int(writer.read_text()))
        except ValueError:
            pass
    output = subprocess.run(["ps", "-axo", "pid=,ppid=,pgid=,comm="],
                            capture_output=True, text=True, check=True, timeout=5)
    rows = {}
    for line in output.stdout.splitlines():
        fields = line.strip().split(None, 3)
        if len(fields) == 4:
            pid, parent, group = map(int, fields[:3])
            rows[pid] = {"pid": pid, "ppid": parent, "pgid": group,
                         "executable": Path(fields[3]).name}
    known.add(root_pid)
    while True:
        found = {pid for pid, row in rows.items() if row["ppid"] in known}
        if found <= known:
            break
        known.update(found)
    return [row for pid, row in rows.items() if pid in known]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--live", action="store_true", required=True)
    p.add_argument("cases", nargs="+", help="harness:scenario pairs")
    args = p.parse_args()
    cases = [item.split(":") for item in args.cases]
    if any(len(c) != 2 or c[0] not in {"codex", "hermes"} or c[1] not in SCENARIOS for c in cases):
        p.error("invalid case")
    turns = sum(2 if c[1] == "resume" else 1 for c in cases)
    if turns > 16:
        p.error("maximum 16 explicitly submitted model turns per suite")
    os.umask(0o077)
    results = []
    destination = ROOT / ".cache/spike" / f"qualification-{time.time_ns()}.json"
    for harness, scenario in cases:
        prep = subprocess.run([sys.executable, str(ROOT / "scripts/spike/prepare.py")],
                              cwd=ROOT, capture_output=True, text=True, timeout=40)
        if prep.returncode:
            print("Preparation failed; no native diagnostics copied", flush=True)
            return 2
        directory = Path(prep.stdout.splitlines()[0].removeprefix("Prepared: "))
        print(f"START {harness}:{scenario} {directory.name}", flush=True)
        command = [str(ROOT / "bin/vigil"), "spike", "--manifest", str(directory / "launch.json"),
                   "--harness", harness, "--live"]
        if scenario != "edit":
            command += ["--scenario", scenario]
        child = subprocess.Popen(command, cwd=ROOT)
        known, samples = set(), {}
        deadline = time.monotonic() + 325
        while child.poll() is None and time.monotonic() < deadline:
            for row in ancestry(child.pid, directory / "fixture", known):
                samples[tuple(row.values())] = row
            time.sleep(.25)
        if child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=20)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
        after = ancestry(child.pid, directory / "fixture", known)
        reports = sorted((directory / "evidence").glob("result-*.json"))
        record = {"harness": harness, "scenario": scenario, "exit_code": child.returncode,
                  "report": str(reports[-1].relative_to(ROOT)) if reports else None,
                  "process_ancestry": list(samples.values()), "processes_after_runner": after}
        if scenario in {"child", "loss"}:
            # Every heartbeat fixture self-terminates in 30 seconds. Preserve evidence.
            time.sleep(31)
            record["processes_after_self_limit"] = ancestry(child.pid, directory / "fixture", known)
            heartbeat = directory / "fixture/heartbeat.txt"
            record["final_heartbeat_lines"] = len(heartbeat.read_text().splitlines()) if heartbeat.exists() else 0
        results.append(record)
        destination.write_text(json.dumps({"turn_budget": turns, "cases": results}, indent=2) + "\n")
        print(f"END {harness}:{scenario} exit={child.returncode}", flush=True)
    print(destination, flush=True)
    return int(any(r["exit_code"] for r in results))


if __name__ == "__main__":
    sys.exit(main())
