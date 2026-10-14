#!/usr/bin/env python3
"""Fail-closed validation of one fresh `go test -json -count=1` invocation.

Checks event structure, run/terminal lifecycle, process status and an explicit
suite manifest. This verifies recorded execution, not correctness of tests or
cryptographic authenticity of an untrusted log. Never infer pass from Output.
See https://go.dev/src/cmd/test2json/main.go for the event protocol.
"""
from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
ACTIONS = {"start", "run", "pause", "cont", "pass", "fail", "skip", "output", "bench"}
TERMINAL = {"pass", "fail", "skip"}


class EvidenceError(ValueError):
    pass


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise EvidenceError(f"duplicate JSON field: {key}")
        value[key] = item
    return value


def pair(value):
    if not isinstance(value, list) or len(value) != 2 or not all(isinstance(v, str) and v for v in value):
        raise EvidenceError("test identity must be [package, test]")
    if not re.fullmatch(r"[A-Za-z0-9_./-]+", value[0]) or not re.fullmatch(r"Test[A-Za-z0-9_]+", value[1]):
        raise EvidenceError("mandatory identities must be exact package and top-level Test names")
    return tuple(value)


def load_suite(path: Path, name: str):
    document = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    if not isinstance(document, dict) or type(document.get("version")) is not int or document["version"] != 1 or not isinstance(document.get("suites"), dict):
        raise EvidenceError("invalid evidence manifest")
    suite = document["suites"].get(name)
    if not isinstance(suite, dict) or set(suite) != {"required_tests", "allowed_skips"}:
        raise EvidenceError(f"unknown or malformed suite: {name}")
    if not isinstance(suite["required_tests"], list) or not suite["required_tests"]:
        raise EvidenceError("required test list must not be empty")
    required = [pair(v) for v in suite["required_tests"]]
    if len(set(required)) != len(required):
        raise EvidenceError("duplicate required test")
    allowed = {}
    if not isinstance(suite["allowed_skips"], list):
        raise EvidenceError("allowed_skips must be a list")
    for entry in suite["allowed_skips"]:
        if not isinstance(entry, dict) or set(entry) != {"identity", "reason"} or not isinstance(entry["reason"], str) or not entry["reason"].strip():
            raise EvidenceError("every allowed skip needs an exact identity and documented reason")
        identity = pair(entry["identity"])
        if identity in allowed or identity in required:
            raise EvidenceError("duplicate skip or required/skip overlap")
        allowed[identity] = entry["reason"]
    return set(required), allowed


def evaluate(lines, required, allowed, exit_code):
    packages, tests = {}, {}
    errors = []
    event_count = 0

    def problem(text):
        if len(errors) < 100:
            errors.append(text)

    if type(exit_code) is not int or exit_code != 0:
        problem(f"go test process exit was {exit_code}, not 0")
    for number, line in enumerate(lines, 1):
        event_count += 1
        try:
            event = json.loads(line, object_pairs_hook=unique_object)
        except (ValueError, TypeError) as exc:
            problem(f"line {number}: malformed event ({type(exc).__name__})")
            continue
        if not isinstance(event, dict):
            problem(f"line {number}: event is not an object")
            continue
        action, package, test = event.get("Action"), event.get("Package"), event.get("Test")
        if not isinstance(action, str) or action not in ACTIONS or not isinstance(package, str) or not package:
            problem(f"line {number}: missing/unknown Action or Package")
            continue
        if "Test" in event and (not isinstance(test, str) or not test):
            problem(f"line {number}: invalid Test identity")
            continue
        if action == "output":
            if not isinstance(event.get("Output"), str):
                problem(f"line {number}: output event lacks text")
            continue
        if event.get("FailedBuild"):
            problem(f"build failed for package {package}")
        if action == "start":
            if test or package in packages:
                problem(f"line {number}: duplicate/invalid package start {package}")
            else:
                packages[package] = "running"
            continue
        if packages.get(package) != "running":
            problem(f"line {number}: event outside active package {package}")
            continue
        if not test:
            if action not in TERMINAL:
                problem(f"line {number}: invalid package action {action}")
                continue
            if any(p == package and state not in TERMINAL for (p, _), state in tests.items()):
                problem(f"package {package} ended with unfinished tests")
            packages[package] = action
            if action == "fail":
                problem(f"package failed: {package}")
            if action == "skip" and any(p == package for p, _ in tests):
                problem(f"nonempty package reported skipped: {package}")
            continue
        identity = (package, test)
        old = tests.get(identity)
        if action == "run":
            if old is not None:
                problem(f"duplicate test run: {package}::{test}")
            else:
                tests[identity] = "running"
        elif action == "pause" and old == "running":
            tests[identity] = "paused"
        elif action == "cont" and old == "paused":
            tests[identity] = "running"
        elif action in TERMINAL and old == "running":
            tests[identity] = action
            if action == "fail":
                problem(f"test failed: {package}::{test}")
            elif action == "skip" and identity not in allowed:
                problem(f"unexpected skipped test: {package}::{test}")
        else:
            problem(f"invalid test transition {old!r}->{action}: {package}::{test}")
    if not event_count:
        problem("empty test log")
    if not tests:
        problem("no tests actually ran")
    for package, state in packages.items():
        if state == "running":
            problem(f"missing package completion: {package}")
    for (package, test), state in tests.items():
        if state not in TERMINAL:
            problem(f"missing test completion: {package}::{test}")
    missing = sorted(identity for identity in required if tests.get(identity) != "pass" or packages.get(identity[0]) != "pass")
    for package, test in missing:
        problem(f"mandatory test did not pass in a successful package: {package}::{test}")
    counts = Counter(tests.values())
    return {
        "status": "pass" if not errors else "fail", "events": event_count,
        "packages": len(packages), "tests_started": len(tests),
        "tests_passed": counts["pass"], "tests_failed": counts["fail"],
        "tests_skipped": counts["skip"], "mandatory_tests": len(required),
        "mandatory_missing_or_not_passed": len(missing),
        "allowed_skips_observed": [{"package": p, "test": t, "reason": allowed[(p, t)]}
                                   for (p, t), state in sorted(tests.items()) if state == "skip" and (p, t) in allowed],
        "errors": errors,
    }


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--log", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, default=ROOT / "scripts/required_go_tests.json")
    parser.add_argument("--suite", required=True)
    parser.add_argument("--exit-code", type=int, required=True)
    parser.add_argument("--source-sha", required=True)
    args = parser.parse_args()
    try:
        if not re.fullmatch(r"[0-9a-f]{40}", args.source_sha):
            raise EvidenceError("source-sha must be an exact lowercase 40-hex Git commit")
        required, allowed = load_suite(args.manifest, args.suite)
        with args.log.open(encoding="utf-8") as stream:
            result = evaluate(stream, required, allowed, args.exit_code)
        result.update({"suite": args.suite, "source_sha": args.source_sha,
                       "log_sha256": digest(args.log), "manifest_sha256": digest(args.manifest),
                       "process_exit_code": args.exit_code})
    except (OSError, UnicodeError, ValueError) as exc:
        result = {"status": "fail", "errors": [str(exc)]}
    print(json.dumps(result, ensure_ascii=False, sort_keys=True, indent=2))
    return 0 if result["status"] == "pass" else 1


if __name__ == "__main__":
    raise SystemExit(main())
