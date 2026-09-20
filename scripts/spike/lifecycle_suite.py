#!/usr/bin/env python3
"""Explicit, sequential Stage 3 experiments. No retries; fresh preparation per case."""
import argparse
import json
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
SCENARIOS = {"resume", "interrupt", "child", "loss", "clarify", "approval-allow", "approval-deny"}

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--live", action="store_true", required=True)
    p.add_argument("cases", nargs="+", help="harness:scenario pairs")
    args = p.parse_args()
    cases = [item.split(":") for item in args.cases]
    if any(len(c)!=2 or c[0] not in {"codex","hermes"} or c[1] not in SCENARIOS for c in cases):
        p.error("invalid case")
    if sum(2 if c[1]=="resume" else 1 for c in cases)>16:
        p.error("maximum 16 explicitly submitted model turns per suite")
    results=[]
    for harness,scenario in cases:
        prep=subprocess.run([sys.executable,str(ROOT/"scripts/spike/prepare.py")],cwd=ROOT,capture_output=True,text=True,timeout=40,check=True)
        directory=Path(prep.stdout.splitlines()[0].removeprefix("Prepared: "))
        print(f"START {harness}:{scenario} {directory.name}",flush=True)
        child=subprocess.Popen([str(ROOT/"bin/vigil"),"spike","--manifest",str(directory/"launch.json"),"--harness",harness,"--live","--scenario",scenario],cwd=ROOT)
        try:
            code=child.wait(timeout=325)
        except subprocess.TimeoutExpired:
            child.terminate()
            try: code=child.wait(timeout=20)
            except subprocess.TimeoutExpired: child.kill();code=child.wait()
        reports=sorted((directory/"evidence").glob("result-*.json"))
        results.append({"harness":harness,"scenario":scenario,"exit_code":code,"report":str(reports[-1].relative_to(ROOT)) if reports else None})
        print(f"END {harness}:{scenario} exit={code}",flush=True)
        if scenario in {"child","loss"} and code:
            # Fixture child has a 30s lifetime even if native teardown is defective.
            time.sleep(31)
    destination=ROOT/".cache/spike"/f"suite-{time.time_ns()}.json"
    destination.write_text(json.dumps(results,indent=2)+"\n")
    destination.chmod(0o600)
    print(destination)
    return int(any(r["exit_code"] for r in results))

if __name__=="__main__":
    sys.exit(main())
