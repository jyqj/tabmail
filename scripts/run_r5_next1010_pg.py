#!/usr/bin/env python3
"""Compare unchanged NEXT1010 regression bytes on baseline and reviewed source."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

BASELINE = "74d5fc72f7ad2e086bd3060b78c548dc302d0408"
CASES = {
    "refresh-issuance": (["internal/api/handlers/r5_refresh_issuance_pg_test.go", "internal/api/handlers/r5_refresh_issuance_test.go"], "./internal/api/handlers", "^TestR5RefreshIssuancePostgres", 9),
    "permission-audit": (["internal/store/postgres/r5_permission_audit_visibility_test.go"], "./internal/store/postgres", "^TestR5PermissionAudit", 10),
}


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def snapshot(root, paths):
    return {"commit": git(root, "rev-parse", "HEAD"),
            "tree": git(root, "rev-parse", "HEAD^{tree}"),
            "status": git(root, "status", "--porcelain", "--untracked-files=all"),
            "regression_sha256": {path: hashlib.sha256((root / path).read_bytes()).hexdigest() for path in paths}}


def execute(root, output, paths, package, pattern):
    output.mkdir()
    before = snapshot(root, paths)
    command = ["go", "test", "-mod=readonly", "-race", "-count=1", "-timeout=180s", "-json", package, "-run", pattern]
    with (output / "go-test.jsonl").open("w") as stdout, (output / "go-test.stderr").open("w") as stderr:
        result = subprocess.run(command, cwd=root, stdout=stdout, stderr=stderr, timeout=360)
    rows = [json.loads(line) for line in (output / "go-test.jsonl").read_text().splitlines()]
    started = {row["Test"] for row in rows if row.get("Test") and row["Action"] == "run"}
    terminal = {row["Test"]: row["Action"] for row in rows
                if row.get("Test") and row["Action"] in ("pass", "fail", "skip")}
    leaves = {name: state for name, state in terminal.items()
              if not any(other.startswith(name + "/") for other in terminal)}
    failures = {name: "".join(row.get("Output", "") for row in rows if row.get("Test") == name)[-3000:]
                for name, state in terminal.items() if state != "pass"}
    after = snapshot(root, paths)
    summary = {"source_before": before, "source_after": after, "source_stable": before == after,
               "command": command, "exit_code": result.returncode, "leaves": leaves,
               "pass": sum(state == "pass" for state in leaves.values()),
               "fail": sum(state == "fail" for state in leaves.values()),
               "all_skips": sum(row["Action"] == "skip" for row in rows),
               "missing_terminals": sorted(started - terminal.keys()),
               "failure_output": failures,
               "stderr_tail": (output / "go-test.stderr").read_text()[-3000:],
               "log_sha256": hashlib.sha256((output / "go-test.jsonl").read_bytes()).hexdigest()}
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    if before != after:
        raise RuntimeError("test source changed during execution")
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--case", required=True, choices=CASES)
    args = parser.parse_args()
    if not os.environ.get("TABMAIL_TEST_DB_DSN"):
        parser.error("an explicit disposable TABMAIL_TEST_DB_DSN is required")
    root, output = args.candidate.resolve(), args.output.resolve()
    if git(root, "status", "--porcelain", "--untracked-files=all"):
        parser.error("candidate must be the exact clean committed source")
    output.mkdir(parents=True, exist_ok=False)
    paths, package, pattern, expected = CASES[args.case]
    frozen = {path: (root / path).read_bytes() for path in paths}
    baseline = output / "baseline-source"
    subprocess.run(["git", "-C", str(root), "worktree", "add", "--detach", str(baseline), BASELINE], check=True)
    try:
        for path in paths:
            shutil.copyfile(root / path, baseline / path)
        old = execute(baseline, output / "baseline", paths, package, pattern)
        new = execute(root, output / "candidate", paths, package, pattern)
        report = {"case": args.case, "baseline": old, "candidate": new}
        (output / "comparison.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps(report, sort_keys=True), flush=True)
        if any(value != (root / path).read_bytes() or value != (baseline / path).read_bytes() for path, value in frozen.items()):
            raise RuntimeError("regression bytes changed between sources")
        if not old["leaves"] or old["fail"] == 0 or old["exit_code"] == 0:
            raise RuntimeError("baseline did not reproduce a test failure")
        if len(old["leaves"]) != expected or set(old["leaves"]) != set(new["leaves"]):
            raise RuntimeError("baseline and candidate did not execute the same test leaves")
        if old["all_skips"] or new["all_skips"] or old["missing_terminals"] or new["missing_terminals"]:
            raise RuntimeError("skipped or missing tests cannot establish acceptance")
        if new["exit_code"] or new["fail"] or new["failure_output"]:
            raise RuntimeError("candidate regression suite failed")
    finally:
        subprocess.run(["git", "-C", str(root), "worktree", "remove", "--force", str(baseline)], check=True)


if __name__ == "__main__":
    main()
