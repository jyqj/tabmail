"""Recover public fixed SMTPUTF8 wire evidence on the original and repaired sources.

This executes real owned TCP/TLS peers with FakeStore recipient persistence.
The 109 frozen tests are unchanged. This is not a PostgreSQL or release gate.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess

BASELINE = "ff43326e81f262daaac7b3dfd8b3047a8f51743b"
TEST = "internal/outbound/r5_exec_smtputf8_test.go"
TEST_HASH = "03047b4ba6b9c4590e032dfc58eeada6750e3b2e4c7aa80b96859ab98354e372"
PACKAGE = "tabmail/internal/outbound"
MARKER = "EXEC09_SENT_WITHOUT_SMTPUTF8"
MODES = ("relay_plain", "relay_implicit_tls", "relay_starttls", "direct_plain", "direct_starttls")
PAYLOADS = ("ascii_quoted_alabel", "unicode_sender_envelope_only", "unicode_recipient_envelope_only",
            "unicode_domain_envelope_only", "unicode_last_bcc", "unicode_top_level_header",
            "encoded_headers_body_and_attachment", "eight_bit_body_only")
CAPABILITIES = "TestExecSMTPUTF8Capabilities"
REFRESH = "TestExecSMTPUTF8RefreshAfterSTARTTLS"
HELO = "TestExecSMTPUTF8HELOFallback"
NEXT_MX = "TestExecSMTPUTF8TriesNextMXBeforeSending"
LEDGER = "TestExecSMTPUTF8SubmissionRecipientLedger"
PARENTS = {CAPABILITIES, REFRESH, HELO, LEDGER}
LEAVES, NEGATIVE = set(), set()
for mode in MODES:
    for capable in ("false", "true"):
        for payload in PAYLOADS:
            name = f"{CAPABILITIES}/{mode}/capable={capable}/{payload}"
            LEAVES.add(name)
            if capable == "false" and payload.startswith("unicode_"):
                NEGATIVE.add(name)
for mode in ("relay_starttls", "direct_starttls"):
    for capable in ("false", "true"):
        for payload in ("unicode_last_bcc", "encoded_headers_body_and_attachment"):
            name = f"{REFRESH}/{mode}/final_capable={capable}/{payload}"
            LEAVES.add(name)
            if capable == "false" and payload == "unicode_last_bcc":
                NEGATIVE.add(name)
for mode in ("relay_plain", "direct_plain"):
    for payload in ("ascii_quoted_alabel", "unicode_sender_envelope_only"):
        name = f"{HELO}/{mode}/{payload}"
        LEAVES.add(name)
        if payload == "unicode_sender_envelope_only":
            NEGATIVE.add(name)
    for capable in ("false", "true"):
        for role in ("to", "cc", "bcc", "header"):
            name = f"{LEDGER}/{mode}/capable={capable}/{role}"
            LEAVES.add(name)
            if capable == "false":
                NEGATIVE.add(name)
LEAVES.add(NEXT_MX)
NEGATIVE.add(NEXT_MX)
POSITIVE = LEAVES - NEGATIVE


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def identity(root):
    if git(root, "status", "--porcelain=v1", "--untracked-files=all"):
        raise ValueError("regression source must be clean")
    if hashlib.sha256((root / TEST).read_bytes()).hexdigest() != TEST_HASH:
        raise ValueError("frozen SMTPUTF8 regression bytes differ")
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
        "commit", "--no-gpg-sign", "-m", "Overlay unchanged SMTPUTF8 regression on fixed public baseline")
    if git(baseline, "rev-parse", "HEAD^") != BASELINE or git(baseline, "diff", "--name-only", BASELINE, "HEAD") != TEST:
        raise ValueError("baseline overlay changed anything other than the frozen test")
    return identity(baseline)


def verify(log, exit_code, baseline):
    events = [json.loads(line) for line in log.read_text().splitlines() if line.strip()]
    if len(LEAVES) != 109 or len(NEGATIVE) != 38:
        raise ValueError("fixed test oracle changed")
    if not events or any(not isinstance(e, dict) or e.get("Package") != PACKAGE for e in events):
        raise ValueError("missing or unrelated SMTPUTF8 execution")
    if any(e.get("FailedBuild") or e.get("Action") not in ("start", "run", "output", "pass", "fail", "skip") for e in events):
        raise ValueError("unexpected build or execution event")
    output = "".join(e.get("Output", "") for e in events if e.get("Action") == "output")
    if any(marker in output for marker in ("WARNING: DATA RACE", "panic:", "fatal error:", "[build failed]")):
        raise ValueError("unrelated runtime or build failure")
    all_ids = LEAVES | PARENTS
    started = [e["Test"] for e in events if e.get("Action") == "run" and e.get("Test")]
    if len(started) != len(set(started)) or set(started) != all_ids:
        raise ValueError("missing, duplicate or unexpected fixed SMTPUTF8 IDs")
    terminal = {}
    for event in events:
        if event.get("Action") not in ("pass", "fail", "skip"):
            continue
        name = event.get("Test", "")
        if name in terminal:
            raise ValueError("duplicate terminal result")
        terminal[name] = event["Action"]
    if set(terminal) != all_ids | {""} or "skip" in terminal.values():
        raise ValueError("unfinished, skipped or unexpected SMTPUTF8 execution")
    assertions = [(event.get("Test", ""), line)
                  for event in events if event.get("Action") == "output"
                  for line in event.get("Output", "").splitlines()
                  if re.match(r"\s*\S+\.go:\d+: ", line)]
    observations = []
    if baseline:
        failures = NEGATIVE | PARENTS | {""}
        if exit_code != 1 or any(state != ("fail" if name in failures else "pass") for name, state in terminal.items()):
            raise ValueError("baseline differs from the 38 declared wire defects and 71 positive controls")
        # Baseline helpers also assert local-error classification and the
        # recipient/job state. They must not hide unrelated setup/cleanup errors.
        allowed = (
            "SMTPUTF8 requirement must produce a local pre-send error, got ",
            MARKER + " MAIL=",
            "SMTPUTF8 mismatch must try the next MX before any DATA: visited=",
            "actual delivery recipient state = ",
            "job sent state disagrees with actual peer capability: capable=",
            "capability check fabricated a server rejection or acceptance: attempts=",
        )
        for name, line in assertions:
            message = re.sub(r"^\s*\S+\.go:\d+: ", "", line)
            if name not in NEGATIVE or not message.startswith(allowed):
                raise ValueError("unrelated assertion outside the declared wire-defect observations")
            if message.startswith(allowed[2]) and name != NEXT_MX:
                raise ValueError("next-MX observation outside its frozen test")
            if message.startswith(allowed[3:]) and not name.startswith(LEDGER + "/"):
                raise ValueError("recipient/job observation outside its frozen test")
        for name in sorted(NEGATIVE):
            lines = [line for test, line in assertions if test == name]
            messages = [re.sub(r"^\s*\S+\.go:\d+: ", "", line) for line in lines]
            expected_prefixes = (allowed[:2] + (allowed[2],) if name == NEXT_MX
                                 else allowed[:2] + allowed[3:] if name.startswith(LEDGER + "/")
                                 else allowed[:2])
            if len(messages) != len(expected_prefixes) or any(
                    sum(message.startswith(prefix) for message in messages) != 1 for prefix in expected_prefixes):
                raise ValueError("missing or duplicate expected baseline assertion")
            marker_lines = [line for line in lines if MARKER in line]
            if len(marker_lines) != 1 or not re.search(
                    r"EXEC09_SENT_WITHOUT_SMTPUTF8 MAIL=\[.+\] RCPT=\[.+\] DATA=1 body_bytes=[1-9]\d*$",
                    marker_lines[0]):
                raise ValueError("negative test lacks one actual MAIL/RCPT/DATA wire observation")
            observations.append({"test": name, "wire": marker_lines[0].strip()})
        if output.count(MARKER) != 38:
            raise ValueError("extra or missing wire observations")
    elif exit_code != 0 or any(state != "pass" for state in terminal.values()) or assertions or MARKER in output:
        raise ValueError("candidate SMTPUTF8 execution did not fully pass")
    return {"started_ids": sorted(all_ids), "leaf_ids": sorted(LEAVES), "leaf_count": 109,
            "passed": sum(terminal[name] == "pass" for name in LEAVES),
            "failed": sum(terminal[name] == "fail" for name in LEAVES), "skipped": 0,
            "exit_code": exit_code, "actual_wire_observations": observations}


def run(root, output, go, baseline):
    before = identity(root)
    if baseline and (git(root, "rev-parse", "HEAD^") != BASELINE or git(root, "diff", "--name-only", BASELINE, "HEAD") != TEST):
        raise ValueError("unexpected baseline source or test overlay")
    output.mkdir(mode=0o700)
    command = [go, "test", "-mod=readonly", "-json", "-race", "-p", "1", "-count=1", "-timeout=120s",
               "-run", "^TestExecSMTPUTF8", "./internal/outbound"]
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
        raise ValueError("source changed during execution or SMTPUTF8 execution timed out")
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
