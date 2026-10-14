"""Execute identical API-key issuance HTTP regressions on PostgreSQL.

The fixed public baseline gains only the frozen new HTTP test. It must expose
nine stale-authority issuances, non-atomic audit, and both missing authority
fences; six compatibility/rollback controls must pass. The candidate must run
and pass all eighteen. This independent gate never exempts the complete existing
PostgreSQL gate or changes its manifest.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

BASELINE = "fdea2178759ce9842844926374ea98517bc4188b"
TEST = "internal/api/handlers/r5_api_key_issuance_pg_test.go"
TEST_HASH = "90fd26b6ce36a867a0ba56da1b32486bf76eb1489ebfa1af52e56afac7def5f9"
PACKAGE = "tabmail/internal/api/handlers"
PARENT = "TestAdvanceAPIKeyIssuancePostgres"
NEGATIVE = {PARENT + "/" + name for name in (
    "employee_freeze", "employee_freeze_unfreeze", "employee_password",
    "employee_promoted", "admin_demoted", "cross_home_super_demoted",
    "permission_revoked", "send_permission_revoked", "zone_permission_revoked",
    "required_audit_rollback", "holds_current_user", "holds_permission_profile")}
POSITIVE = {PARENT + "/" + name for name in (
    "normal_employee", "normal_admin", "normal_cross_home_super",
    "key_insert_rollback", "usage_insert_rollback", "cancellation_after_auth")}


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
        "commit", "--no-gpg-sign", "-m", "Overlay unchanged API key issuance regression on fixed public baseline")
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
            raise ValueError("baseline did not fail for the fixed API key issuance regression")
        if any(terminal[name] != "pass" for name in POSITIVE) or any(terminal[name] != "fail" for name in NEGATIVE):
            raise ValueError("baseline failure set differs from the demonstrated stale authority/atomicity defects")
        for name in NEGATIVE:
            observed = "".join(event.get("Output", "") for event in events
                               if event.get("Action") == "output" and event.get("Test") == name)
            if "/holds_" in name:
                resource = name.split("/holds_", 1)[1]
                marker = "API-key issuer did not fence " + resource + " before INSERT: SQLSTATE=none"
                if any(message in observed for message in (
                    "normal key issue lost atomic result", "returned key does not resolve",
                    "key ownership semantics changed")):
                    raise ValueError("baseline fence probe did not complete a normal issuance")
            elif name.endswith("/required_audit_rollback"):
                marker = "stale or failed API-key issuance escaped: status=201 want=500 counts=[1 1 0] secret_returned=true"
            else:
                marker = "stale or failed API-key issuance escaped: status=201 want=403 counts=[1 1 1] secret_returned=true"
            if marker not in observed:
                raise ValueError("baseline failed without observing the actual issuance defect")
    elif exit_code != 0 or any(state != "pass" for state in terminal.values()):
        raise ValueError("candidate PostgreSQL API key regression did not fully pass")
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
    command = [go, "test", "-mod=readonly", "-json", "-race", "-p", "1", "-count=1", "-timeout=120s",
               "-run", "^" + PARENT + "$", "./internal/api/handlers"]
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
