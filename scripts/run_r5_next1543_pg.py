#!/usr/bin/env python3
"""Run the same new regression bytes on the fixed baseline and candidate."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

BASELINE = "30d2d05ab6ddcc23f47d9b9786885817aef50a0f"
CASES = {
    "mailbox-grants": (
        "internal/store/postgres/r5_frozen_mailbox_grant_test.go",
        "^TestR5FrozenMailboxGrantRevocation$",
        14,
    ),
    "derived-text": (
        "internal/store/postgres/r5_derived_text_persistence_test.go",
        "^TestR5DerivedTextPersistence$",
        8,
    ),
}


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def snapshot(root, regression):
    return {
        "commit": git(root, "rev-parse", "HEAD"),
        "commit_tree": git(root, "rev-parse", "HEAD^{tree}"),
        "dirty_paths": git(root, "status", "--porcelain", "--untracked-files=all"),
        "tracked_diff_sha256": hashlib.sha256(git(root, "diff", "--binary", "HEAD").encode()).hexdigest(),
        "regression_sha256": hashlib.sha256((root / regression).read_bytes()).hexdigest(),
    }


def output_tail(value):
    lines = value.splitlines()
    tail = "\n".join(lines[-12:])
    return {"text": tail[-2000:], "truncated": len(lines) > 12 or len(tail) > 2000}


def failure_output_preview(rows, terminal, leaves):
    # Prefer individual failures; include a parent fail/skip when its children
    # passed so an otherwise invisible setup or cleanup failure is diagnosable.
    names = [name for name, state in leaves.items() if state == "fail"]
    names += [name for name, state in terminal.items()
              if state in {"fail", "skip"} and name not in names]
    previews = []
    for name in names[:8]:
        value = "".join(row.get("Output", "") for row in rows
                        if row.get("Test") == name and row.get("Action") == "output")
        previews.append({"test": name, "action": terminal[name], **output_tail(value)})
    return {
        "max_tests": 8, "max_lines_per_test": 12, "max_chars_per_test": 2000,
        "omitted_tests": max(0, len(names) - len(previews)), "tests": previews,
    }


def execute(root, output, pattern, expected, regression, *, candidate=False):
    output.mkdir(parents=True, exist_ok=False)
    before = snapshot(root, regression)
    if candidate and before["dirty_paths"]:
        raise ValueError("candidate must be the exact clean committed source")
    command = ["go", "test", "-mod=readonly", "-race", "-count=1", "-timeout=120s",
               "-json", "./internal/store/postgres", "-run", pattern]
    with (output / "go-test.jsonl").open("w") as stdout, (output / "go-test.stderr").open("w") as stderr:
        run = subprocess.run(command, cwd=root, stdout=stdout, stderr=stderr, timeout=300)
    (output / "exit-code.txt").write_text(str(run.returncode) + "\n")
    rows = [json.loads(line) for line in (output / "go-test.jsonl").read_text().splitlines()]
    after = snapshot(root, regression)
    terminal = {row["Test"]: row["Action"] for row in rows
                if row.get("Test") and row["Action"] in {"pass", "fail", "skip"}}
    leaves = {name: state for name, state in terminal.items()
              if not any(other.startswith(name + "/") for other in terminal)}
    summary = {
        "source_commit": git(root, "rev-parse", "HEAD"),
        "source_tree": git(root, "rev-parse", "HEAD^{tree}"),
        "dirty_paths": git(root, "status", "--porcelain"),
        "command": command, "exit_code": run.returncode,
        "expected_leaves": expected, "leaves": leaves,
        "pass": sum(value == "pass" for value in leaves.values()),
        "fail": sum(value == "fail" for value in leaves.values()),
        "skip": sum(value == "skip" for value in leaves.values()),
        "all_skip_events": sum(row["Action"] == "skip" for row in rows),
        "failure_output_preview": failure_output_preview(rows, terminal, leaves),
        "stderr_preview": output_tail((output / "go-test.stderr").read_text(errors="replace"))
                          if run.returncode else None,
        "source_before": before, "source_after": after,
        "source_stable": before == after,
        "log_sha256": hashlib.sha256((output / "go-test.jsonl").read_bytes()).hexdigest(),
    }
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    if before != after:
        raise ValueError("execution source changed while the tests ran")
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--case", choices=sorted(CASES), required=True)
    args = parser.parse_args()
    if not os.environ.get("TABMAIL_TEST_DB_DSN"):
        parser.error("explicit disposable TABMAIL_TEST_DB_DSN required")
    candidate = args.candidate.resolve()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    baseline = output / "baseline-source"
    subprocess.run(["git", "-C", str(candidate), "worktree", "add", "--detach", str(baseline), BASELINE], check=True)
    path, pattern, expected = CASES[args.case]
    try:
        original = (candidate / path).read_bytes()
        shutil.copyfile(candidate / path, baseline / path)
        baseline_result = execute(baseline, output / "baseline", pattern, expected, path)
        candidate_result = execute(candidate, output / "candidate", pattern, expected, path, candidate=True)
        if original != (candidate / path).read_bytes() or original != (baseline / path).read_bytes():
            raise ValueError("regression bytes changed during execution")
        comparison = {
            "case": args.case, "regression_path": path,
            "regression_sha256": hashlib.sha256(original).hexdigest(),
            "baseline": baseline_result, "candidate": candidate_result,
        }
        (output / "comparison.json").write_text(json.dumps(comparison, indent=2) + "\n")
        # Print before acceptance checks so rejected PG runs remain diagnosable
        # when artifact download is unavailable. Raw output stays in retained
        # files; only the bounded failure previews are copied into this log.
        print(json.dumps(comparison, sort_keys=True), flush=True)
        if (len(baseline_result["leaves"]) != expected or baseline_result["all_skip_events"]
                or baseline_result["fail"] == 0 or baseline_result["exit_code"] == 0):
            raise ValueError("baseline did not execute the expected failing regression")
        if (candidate_result["exit_code"] or candidate_result["pass"] != expected
                or candidate_result["fail"] or candidate_result["all_skip_events"]
                or len(candidate_result["leaves"]) != expected):
            raise ValueError("candidate did not pass every expected regression leaf")
        if set(baseline_result["leaves"]) != set(candidate_result["leaves"]):
            raise ValueError("baseline and candidate test identities differ")
    finally:
        # This worktree is created by this invocation and contains only the
        # deliberately overlaid regression; neither baseline ref is modified.
        subprocess.run(["git", "-C", str(candidate), "worktree", "remove", "--force", str(baseline)], check=True)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f"PostgreSQL regression verification failed: {type(error).__name__}: {error}", file=sys.stderr)
        raise SystemExit(1)
