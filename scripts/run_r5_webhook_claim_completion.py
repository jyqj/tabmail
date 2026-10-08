"""Run the frozen webhook ownership regression on its baseline and candidate.

This is an additional PostgreSQL regression, not an exemption from the complete
backend gate. The unchanged test bytes must actually execute on both sources.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

BASELINE = "fc00a9d0b087b9390630f0980454f69b91ca2617"
TEST = "internal/store/postgres/r5_webhook_claim_completion_test.go"
TEST_HASH = "dbb0435f481c4aa011477b052a13e2ee9107780ecd48073322f9c2b9d9e2e835"
PACKAGE = "tabmail/internal/store/postgres"
KINDS = ("outbox-done", "outbox-retry", "webhook-done", "webhook-retry", "webhook-dead")


def expected():
    positive, negative = set(), set()
    prefix = "TestR5WebhookClaimCompletion"
    for kind in KINDS:
        positive.add(prefix + "PreservesCurrentGeneration/" + kind)
        for boundary in ("cancelled", "write-rejected"):
            positive.add(prefix + "PreservesCancellationAndDatabaseFailure/" + kind + "/" + boundary)
        for boundary in ("wrong-generation", "zero-generation", "negative-generation", "missing-row",
                         "pending-state", "expired-lease", "null-lease", "reclaimed-processing",
                         "same-generation-completed"):
            negative.add(prefix + "RejectsLostOwnership/" + kind + "/" + boundary)
        negative.add(prefix + "ChecksLeaseAfterRowWait/" + kind)
        terminals = ("done", "retry") if kind.startswith("outbox-") else ("delivered", "retry", "dead")
        for terminal in terminals:
            negative.add(prefix + "PreservesNewerTerminalState/" + kind + "/after-" + terminal)
    return positive, negative


def identity(root):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()
    if git("status", "--porcelain=v1", "--untracked-files=all"):
        raise ValueError("regression source must be clean")
    if hashlib.sha256((root / TEST).read_bytes()).hexdigest() != TEST_HASH:
        raise ValueError("frozen PostgreSQL regression bytes differ")
    return {"commit": git("rev-parse", "HEAD"), "tree": git("rev-parse", "HEAD^{tree}"),
            "test_sha256": TEST_HASH}


def verify(log, exit_code, baseline):
    events = [json.loads(line) for line in log.read_text().splitlines() if line.strip()]
    if not events or any(event.get("Package") != PACKAGE for event in events):
        raise ValueError("missing or unrelated PostgreSQL execution")
    started = [event["Test"] for event in events if event.get("Action") == "run" and event.get("Test")]
    if len(started) != len(set(started)):
        raise ValueError("duplicate test execution")
    positive, negative = expected()
    leaves = positive | negative
    all_ids = set(leaves)
    for leaf in leaves:
        pieces = leaf.split("/")
        all_ids.update("/".join(pieces[:index]) for index in range(1, len(pieces)))
    if set(started) != all_ids:
        raise ValueError("missing or unexpected required PostgreSQL test IDs")
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
        if exit_code != 1 or terminal[""] != "fail":
            raise ValueError("baseline did not fail for the fixed ownership regression")
        if any(terminal[name] != "pass" for name in positive) or any(terminal[name] != "fail" for name in negative):
            raise ValueError("baseline failure set differs from the demonstrated ownership defect")
        markers = {
            "TestR5WebhookClaimCompletionRejectsLostOwnership": "lost ownership did not return ErrClaimLeaseLost: <nil>",
            "TestR5WebhookClaimCompletionPreservesNewerTerminalState": "stale completion did not report lost generation: <nil>",
            "TestR5WebhookClaimCompletionChecksLeaseAfterRowWait": "completion after lease expiry was not fenced: <nil>",
        }
        for name in negative:
            observed = "".join(event.get("Output", "") for event in events
                               if event.get("Action") == "output" and event.get("Test") == name)
            if markers[name.split("/")[0]] not in observed:
                raise ValueError("baseline failed without the required stale-write defect observation")
    elif exit_code != 0 or any(state != "pass" for state in terminal.values()):
        raise ValueError("candidate PostgreSQL ownership regression did not fully pass")
    return {"started_ids": sorted(all_ids), "leaf_count": len(leaves),
            "passed": sum(terminal[name] == "pass" for name in leaves),
            "failed": sum(terminal[name] == "fail" for name in leaves), "skipped": 0,
            "exit_code": exit_code}


def run(root, output, go, baseline):
    before = identity(root)
    if baseline and before["commit"] != BASELINE:
        raise ValueError("unexpected baseline source")
    output.mkdir(mode=0o700)
    command = [go, "test", "-mod=readonly", "-json", "-race", "-count=1", "-timeout=120s",
               "-run", "^TestR5WebhookClaimCompletion", "./internal/store/postgres"]
    log, stderr = output / "go-test.jsonl", output / "go-test.stderr"
    with log.open("wb") as stdout_file, stderr.open("wb") as stderr_file:
        completed = subprocess.run(command, cwd=root, stdout=stdout_file, stderr=stderr_file,
                                   timeout=300, check=False)
    after = identity(root)
    receipt = {"before": before, "after": after, "command": command, "exit_code": completed.returncode,
               "stdout_sha256": hashlib.sha256(log.read_bytes()).hexdigest(),
               "stderr_sha256": hashlib.sha256(stderr.read_bytes()).hexdigest()}
    (output / "execution.json").write_text(json.dumps(receipt, indent=2) + "\n")
    if before != after:
        raise ValueError("regression source changed during execution")
    receipt["results"] = verify(log, completed.returncode, baseline)
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
    results = {}
    errors = {}
    for label, root in (("baseline", baseline), ("candidate", candidate)):
        try:
            results[label] = run(root, output / label, args.go, label == "baseline")
        except Exception as error:
            # Continue with the other source and retain the original failing gate.
            errors[label] = {"type": type(error).__name__, "message": str(error)}
    summary = {"baseline": BASELINE, "test_sha256": TEST_HASH, "results": results, "errors": errors}
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps({"passed": not errors, "results": {name: row["results"] for name, row in results.items()}, "errors": errors}))
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
