"""Verify the same atomic settings seed regression against two real PG sources.

The fixed public baseline may gain only the frozen test. It must demonstrate
two actual stale overwrites while six compatibility controls pass. The current
candidate must pass all eight leaves. This gate does not replace the complete
existing PostgreSQL manifest or grant release qualification.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

BASELINE = "eae219951c6f9cbf17eb4062b69464c731807be7"
TEST = "internal/settings/r5_settings_seed_pg_test.go"
TEST_HASH = "9a6f75b684824d06a763065823e488d961a1e8b033fa69e84de9ad1fe4b2198a"
PACKAGE = "tabmail/internal/settings"
CONCURRENT = "TestR5SettingsSeedPGConcurrentWrites"
PRESERVE = "TestR5SettingsSeedPGPreservesExistingValues"
PARENTS = {CONCURRENT, PRESERVE}
NEGATIVE = {CONCURRENT + "/" + name for name in (
    "concurrent_admin_write", "concurrent_startup_seed")}
POSITIVE = {PRESERVE + "/" + name for name in ("empty", "false", "zero", "text")} | {
    "TestR5SettingsSeedPGCreatesAbsentDefaults", "TestR5SettingsSeedPGCancelledContext"}
MARKER = "EXEC07_STALE_SEED_OVERWROTE_COMMITTED_SETTING"


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def identity(root):
    if git(root, "status", "--porcelain=v1", "--untracked-files=all"):
        raise ValueError("regression source must be clean")
    if hashlib.sha256((root / TEST).read_bytes()).hexdigest() != TEST_HASH:
        raise ValueError("frozen settings PostgreSQL regression bytes differ")
    return {"commit": git(root, "rev-parse", "HEAD"), "tree": git(root, "rev-parse", "HEAD^{tree}"),
            "test_sha256": TEST_HASH}


def overlay_baseline(baseline, candidate):
    identity(candidate)
    if git(baseline, "status", "--porcelain=v1", "--untracked-files=all") or git(baseline, "rev-parse", "HEAD") != BASELINE:
        raise ValueError("expected a clean fixed public baseline before test overlay")
    if git(baseline, "ls-tree", "HEAD", "--", TEST):
        raise ValueError("baseline already contains the regression")
    (baseline / TEST).write_bytes((candidate / TEST).read_bytes())
    git(baseline, "add", "--", TEST)
    git(baseline, "-c", "user.name=Owned regression fixture", "-c", "user.email=fixture@example.invalid",
        "commit", "--no-gpg-sign", "-m", "Overlay unchanged settings seed regression on fixed public baseline")
    if git(baseline, "rev-parse", "HEAD^") != BASELINE or git(baseline, "diff", "--name-only", BASELINE, "HEAD") != TEST:
        raise ValueError("baseline overlay changed anything other than the frozen test")
    return identity(baseline)


def verify(log, exit_code, baseline):
    events = [json.loads(line) for line in log.read_text().splitlines() if line.strip()]
    if not events or any(not isinstance(e, dict) or e.get("Package") != PACKAGE for e in events):
        raise ValueError("missing or unrelated PostgreSQL execution")
    if any(e.get("FailedBuild") or e.get("Action") not in ("start", "run", "output", "pass", "fail", "skip") for e in events):
        raise ValueError("unexpected build or execution event")
    output = "".join(e.get("Output", "") for e in events if e.get("Action") == "output")
    if any(marker in output for marker in ("WARNING: DATA RACE", "panic:", "fatal error:", "[build failed]")):
        raise ValueError("unrelated runtime or build failure")
    all_ids = POSITIVE | NEGATIVE | PARENTS
    started = [e["Test"] for e in events if e.get("Action") == "run" and e.get("Test")]
    if len(started) != len(set(started)) or set(started) != all_ids:
        raise ValueError("missing, duplicate or unexpected required PostgreSQL test IDs")
    terminal = {}
    for event in events:
        if event.get("Action") not in ("pass", "fail", "skip"):
            continue
        name = event.get("Test", "")
        if name in terminal:
            raise ValueError("duplicate terminal result")
        terminal[name] = event["Action"]
    if set(terminal) != all_ids | {""} or "skip" in terminal.values():
        raise ValueError("unfinished, skipped or unexpected PostgreSQL execution")
    # Parent/package results already fail on the baseline. An additional
    # assertion in either scope must not be hidden behind that expected fail.
    source_assertions = [(event.get("Test", ""), line)
                         for event in events if event.get("Action") == "output"
                         for line in event.get("Output", "").splitlines()
                         if re.match(r"\s*\S+\.go:\d+: ", line)]
    if baseline:
        if len(source_assertions) != 2 or {name for name, _ in source_assertions} != NEGATIVE:
            raise ValueError("unexpected source assertion outside the two declared overwrite observations")
    elif source_assertions:
        raise ValueError("candidate execution contains an unexpected source assertion")
    observations = []
    if baseline:
        failures = NEGATIVE | {CONCURRENT, ""}
        if exit_code != 1 or any(state != ("fail" if name in failures else "pass") for name, state in terminal.items()):
            raise ValueError("baseline failure set differs from the two actual stale seed overwrites")
        for name in sorted(NEGATIVE):
            writer = name.rsplit("/", 1)[1]
            observed = "".join(e.get("Output", "") for e in events
                               if e.get("Action") == "output" and e.get("Test") == name)
            expected = (MARKER + " writer=" + writer + ' before_value="false" after_value="true" '
                        'before_description="' + writer + ' committed choice" '
                        'after_description="first startup environment" before_updated_at=')
            # A cleanup/connection/assertion failure alongside the overwrite is
            # still a failed proof. Frozen tests log exactly one assertion for
            # each demonstrated stale write; reject additional source assertions.
            assertions = [line for line in observed.splitlines() if re.match(r"\s*\S+\.go:\d+: ", line)]
            if observed.count(MARKER) != 1 or len(assertions) != 1 or expected not in assertions[0]:
                raise ValueError("baseline failed without exclusively observing the actual overwrite")
            if not re.search(r"before_updated_at=\d{4}-\S+ after_updated_at=\d{4}-\S+$", assertions[0]):
                raise ValueError("baseline overwrite lacks persisted timestamp evidence")
            observations.append(assertions[0].strip())
    elif exit_code != 0 or any(state != "pass" for state in terminal.values()):
        raise ValueError("candidate settings seed PostgreSQL regression did not fully pass")
    leaves = POSITIVE | NEGATIVE
    return {"started_ids": sorted(all_ids), "leaf_count": len(leaves),
            "passed": sum(terminal[name] == "pass" for name in leaves),
            "failed": sum(terminal[name] == "fail" for name in leaves), "skipped": 0,
            "exit_code": exit_code, "actual_overwrite_observations": observations}


def run(root, output, go, baseline):
    before = identity(root)
    if baseline and (git(root, "rev-parse", "HEAD^") != BASELINE or git(root, "diff", "--name-only", BASELINE, "HEAD") != TEST):
        raise ValueError("unexpected baseline source or test overlay")
    output.mkdir(mode=0o700)
    command = [go, "test", "-mod=readonly", "-json", "-race", "-p", "1", "-count=1", "-timeout=120s",
               "-run", "^TestR5SettingsSeedPG", "./internal/settings"]
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
