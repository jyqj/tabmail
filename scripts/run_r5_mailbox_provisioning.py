"""Execute identical mailbox HTTP regressions against two owned PostgreSQL sources.

The baseline differs from the fixed public source only by this new test file.
Its unrelated INSERT/audit uniqueness failures must incorrectly reach HTTP409,
and its address conflict must lose the original PostgreSQL cause; all seven
controls must pass. The candidate must execute and pass all ten. This regression
does not exempt either source from the existing complete PostgreSQL gate.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

BASELINE = "be3a6bf41daa198a39306c90fd03411e38a017d8"
TEST = "internal/store/postgres/r5_mailbox_provision_conflict_pg_test.go"
TEST_HASH = "575a03170f48a6fb15edaa34e5049950efce625e11da5b2f712edf87b75603e2"
PACKAGE = "tabmail/internal/store/postgres"
PARENT = "TestR5MailboxProvisionConflictPostgres"
NEGATIVE = {PARENT + "/" + name for name in (
    "unknown_unique_error", "audit_unique_failure", "real_cause_preserved")}
POSITIVE = {PARENT + "/" + name for name in (
    "shared_duplicate", "normalized_duplicate", "personal_duplicate", "new_address",
    "invalid_address", "employee_denied", "audit_rollback")}


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def identity(root):
    if git(root, "status", "--porcelain=v1", "--untracked-files=all"):
        raise ValueError("regression source must be clean")
    if hashlib.sha256((root / TEST).read_bytes()).hexdigest() != TEST_HASH:
        raise ValueError("frozen PostgreSQL regression bytes differ")
    return {"commit": git(root, "rev-parse", "HEAD"), "tree": git(root, "rev-parse", "HEAD^{tree}"),
            "test_sha256": TEST_HASH}


def overlay_baseline(baseline, candidate):
    identity(candidate)
    if git(baseline, "status", "--porcelain=v1", "--untracked-files=all") or git(baseline, "rev-parse", "HEAD") != BASELINE:
        raise ValueError("expected a clean fixed public baseline before test overlay")
    if git(baseline, "ls-tree", "HEAD", "--", TEST):
        raise ValueError("fixed baseline unexpectedly already contains the new regression")
    (baseline / TEST).write_bytes((candidate / TEST).read_bytes())
    git(baseline, "add", "--", TEST)
    git(baseline, "-c", "user.name=Owned regression fixture", "-c", "user.email=fixture@example.invalid",
        "commit", "--no-gpg-sign", "-m", "Overlay unchanged mailbox regression on fixed public baseline")
    if git(baseline, "rev-parse", "HEAD^") != BASELINE or git(baseline, "diff", "--name-only", BASELINE, "HEAD") != TEST:
        raise ValueError("baseline overlay changed anything other than the frozen test")
    return identity(baseline)


def verify(log, exit_code, baseline):
    events = [json.loads(line) for line in log.read_text().splitlines() if line.strip()]
    if not events or any(event.get("Package") != PACKAGE for event in events):
        raise ValueError("missing or unrelated PostgreSQL execution")
    started = [event["Test"] for event in events if event.get("Action") == "run" and event.get("Test")]
    all_ids = POSITIVE | NEGATIVE | {PARENT}
    if len(started) != len(set(started)) or set(started) != all_ids:
        raise ValueError("missing, duplicate or unexpected required PostgreSQL test IDs")
    terminal = {}
    for event in events:
        if event.get("Action") not in ("pass", "fail", "skip"):
            continue
        name = event.get("Test", "")
        if name in terminal:
            raise ValueError("duplicate terminal execution result")
        terminal[name] = event["Action"]
    if set(terminal) != all_ids | {""} or "skip" in terminal.values():
        raise ValueError("unfinished, skipped or unexpected PostgreSQL execution")
    if baseline:
        if exit_code != 1 or terminal[""] != "fail" or terminal[PARENT] != "fail":
            raise ValueError("baseline did not fail for the fixed address-conflict regression")
        if any(terminal[name] != "pass" for name in POSITIVE) or any(terminal[name] != "fail" for name in NEGATIVE):
            raise ValueError("baseline failure set differs from the demonstrated transaction error defects")
        for name in NEGATIVE:
            observed = "".join(event.get("Output", "") for event in events
                               if event.get("Action") == "output" and event.get("Test") == name)
            marker = ("actual address conflict lost original PostgreSQL cause: "
                      if name.endswith("/real_cause_preserved") else
                      "unrelated storage failure must be HTTP500; actual=409")
            if marker not in observed:
                raise ValueError("baseline failed without observing the actual error-classification defect")
    elif exit_code != 0 or any(state != "pass" for state in terminal.values()):
        raise ValueError("candidate PostgreSQL mailbox regression did not fully pass")
    leaves = POSITIVE | NEGATIVE
    return {"started_ids": sorted(all_ids), "leaf_count": len(leaves),
            "passed": sum(terminal[name] == "pass" for name in leaves),
            "failed": sum(terminal[name] == "fail" for name in leaves), "skipped": 0,
            "exit_code": exit_code}


def run(root, output, go, baseline):
    before = identity(root)
    if baseline and (git(root, "rev-parse", "HEAD^") != BASELINE or git(root, "diff", "--name-only", BASELINE, "HEAD") != TEST):
        raise ValueError("unexpected baseline source or test overlay")
    output.mkdir(mode=0o700)
    command = [go, "test", "-mod=readonly", "-json", "-race", "-count=1", "-timeout=120s",
               "-run", "^" + PARENT + "$", "./internal/store/postgres"]
    log, stderr = output / "go-test.jsonl", output / "go-test.stderr"
    timed_out = False
    with log.open("wb") as stdout_file, stderr.open("wb") as stderr_file:
        try:
            completed = subprocess.run(command, cwd=root, stdout=stdout_file, stderr=stderr_file,
                                       timeout=300, check=False)
            exit_code = completed.returncode
        except subprocess.TimeoutExpired:
            timed_out, exit_code = True, 124
    after = identity(root)
    receipt = {"before": before, "after": after, "command": command, "exit_code": exit_code,
               "timed_out": timed_out, "stdout_sha256": hashlib.sha256(log.read_bytes()).hexdigest(),
               "stderr_sha256": hashlib.sha256(stderr.read_bytes()).hexdigest()}
    (output / "execution.json").write_text(json.dumps(receipt, indent=2) + "\n")
    if before != after or timed_out:
        raise ValueError("source changed during execution or PostgreSQL execution timed out")
    receipt["results"] = verify(log, exit_code, baseline)
    (output / "verified.json").write_text(json.dumps(receipt, indent=2) + "\n")
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    if not os.environ.get("TABMAIL_TEST_DB_DSN"):
        raise ValueError("explicit disposable PostgreSQL DSN required")
    baseline, candidate, output = args.baseline.resolve(), args.candidate.resolve(), args.output.resolve()
    if baseline == candidate or any(output.is_relative_to(root) for root in (baseline, candidate)):
        raise ValueError("separate sources and external evidence directory required")
    output.mkdir(mode=0o700)
    results, errors = {}, {}
    try:
        overlay_baseline(baseline, candidate)
    except Exception as error:
        errors["overlay"] = {"type": type(error).__name__, "message": str(error)}
    for label, root in (("baseline", baseline), ("candidate", candidate)):
        try:
            results[label] = run(root, output / label, args.go, label == "baseline")
        except Exception as error:
            errors[label] = {"type": type(error).__name__, "message": str(error)}
    summary = {"baseline": BASELINE, "test_sha256": TEST_HASH, "results": results, "errors": errors}
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps({"passed": not errors, "results": {name: row["results"] for name, row in results.items()}, "errors": errors}))
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
