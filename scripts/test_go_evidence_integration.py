#!/usr/bin/env python3
"""Run real Go processes proving that missing execution cannot pass the gate.

The three original zero-exit counterexamples remain distinct from the current
explicit missing-DSN hard failure. Expected rejection is never product success.

Run after dependency/cache preparation. No database credentials or browser flag
are passed to these subprocesses; no production service is contacted. Outputs
use exclusive creation so earlier evidence cannot be silently overwritten.
"""
from __future__ import annotations
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = "tabmail/internal/store/postgres"
CASES = [
    {"name": "missing-dsn", "suite": "backend", "test": "TestR5SchemaInventoryAndRestart",
     "pattern": "^TestR5SchemaInventoryAndRestart$", "go_exit": 0, "terminal": "skip",
     "diagnostic": "TABMAIL_TEST_DB_DSN is required for real PostgreSQL integration tests",
     "category": "zero_exit_missing_mandatory_database_execution"},
    {"name": "zero-tests", "suite": "backend", "test": None,
     "pattern": "^TestR5DefinitelyNoSuchTest$", "go_exit": 0, "terminal": None,
     "diagnostic": None, "category": "zero_exit_no_tests_executed"},
    {"name": "browser-disabled", "suite": "browser", "test": "TestR3BrowserJourney",
     "pattern": "^TestR3BrowserJourney$", "go_exit": 0, "terminal": "skip",
     "diagnostic": "run in the browser-journey CI job",
     "category": "zero_exit_missing_mandatory_browser_execution"},
    {"name": "current-missing-dsn-hardfail", "suite": "backend",
     "test": "TestR5PermissionAuthorityMatrixNonInteractive",
     "pattern": "^TestR5PermissionAuthorityMatrixNonInteractive$", "go_exit": 1, "terminal": "fail",
     "diagnostic": "R5 authority matrix requires owned TABMAIL_TEST_DB_DSN; no skip",
     "category": "current_explicit_missing_database_rejected"},
]


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate raw Go JSON field")
        value[key] = item
    return value


def write_exclusive(path, value):
    with path.open("x", encoding="utf-8") as stream:
        stream.write(value)


def validate_negative(case, text, stderr, go_exit, gate_exit, report):
    """Validate a counterexample, never turn a failing backend into product PASS."""
    if go_exit != case["go_exit"]:
        raise ValueError(f"{case['name']}: Go exit {go_exit}, expected {case['go_exit']}")
    if stderr.strip():
        raise ValueError(f"{case['name']}: unexpected subprocess stderr")
    events = [json.loads(line, object_pairs_hook=unique_object) for line in text.splitlines() if line.strip()]
    if not events:
        raise ValueError("empty raw Go execution log")
    lifecycle, package_lifecycle, diagnostics = [], [], []
    combined = ""
    for event in events:
        if not isinstance(event, dict) or event.get("Package") != PACKAGE or event.get("FailedBuild"):
            raise ValueError("unexpected package/build event")
        action, test = event.get("Action"), event.get("Test")
        if action not in {"start", "run", "pass", "fail", "skip", "output"}:
            raise ValueError("unexpected raw event action")
        if test is not None and test != case["test"]:
            raise ValueError("unexpected additional test or subtest")
        if action == "output":
            output = event.get("Output")
            if not isinstance(output, str):
                raise ValueError("raw output missing text")
            combined += output
            for message in re.findall(r"[\w.-]+\.go:\d+: ([^\n]*)", output):
                if test != case["test"] or message != case["diagnostic"]:
                    raise ValueError("non-target assertion/setup diagnostic")
                diagnostics.append(message)
        elif test is not None:
            lifecycle.append(action)
        else:
            package_lifecycle.append(action)
    if any(marker in combined for marker in ["WARNING: DATA RACE", "panic:", "runtime error:",
                                             "fatal error:", "test timed out", "[build failed]",
                                             "context deadline exceeded"]):
        raise ValueError("infrastructure/race/panic/timeout is not a negative counterexample")
    expected_package = ["start", "fail"] if case["go_exit"] == 1 else ["start", "pass"]
    # A no-tests package may use a skip terminal; it is not a test skip.
    if case["test"] is None and package_lifecycle == ["start", "skip"]:
        expected_package = package_lifecycle
    expected_test = ["run", case["terminal"]] if case["test"] else []
    if package_lifecycle != expected_package or lifecycle != expected_test:
        raise ValueError("missing/duplicate/unexpected exact raw lifecycle")
    if diagnostics != ([case["diagnostic"]] if case["diagnostic"] else []):
        raise ValueError("missing/duplicate exact negative diagnostic")
    started = 1 if case["test"] else 0
    failed = 1 if case["terminal"] == "fail" else 0
    skipped = 1 if case["terminal"] == "skip" else 0
    if (gate_exit != 1 or report.get("status") != "fail"
            or report.get("process_exit_code") != go_exit
            or report.get("tests_started") != started
            or report.get("tests_failed") != failed
            or report.get("tests_skipped") != skipped
            or report.get("mandatory_missing_or_not_passed", 0) < 1):
        raise ValueError("strict gate did not reject the exact recorded counterexample")
    known = {f"go test process exit was {go_exit}, not 0", "no tests actually ran",
             f"package failed: {PACKAGE}", f"test failed: {PACKAGE}::{case['test']}",
             f"unexpected skipped test: {PACKAGE}::{case['test']}"}
    errors = report.get("errors")
    if (not isinstance(errors, list) or not errors
            or any(not isinstance(error, str) or (error not in known and not error.startswith(
                "mandatory test did not pass in a successful package: ")) for error in errors)):
        raise ValueError("unknown strict-gate rejection; nonzero exit alone proves nothing")
    return {"status": "expected_negative_rejection", "category": case["category"],
            "go_process_exit": go_exit, "gate_process_exit": gate_exit,
            "unknown_failures": 0, "product_green": False, "task_complete": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.source_sha):
        parser.error("source-sha must be an exact 40-hex Git commit")
    env = dict(os.environ)
    for key in ("TABMAIL_TEST_DB_DSN", "TABMAIL_BROWSER_E2E", "TABMAIL_BROWSER_FIXTURE",
                "TABMAIL_E2E_BROWSER_MODULE", "TABMAIL_BROWSER_EVIDENCE", "PGPASSWORD", "PGSERVICE"):
        env.pop(key, None)
    try:
        manifest = json.loads((ROOT / "scripts/required_go_tests.json").read_text(), object_pairs_hook=unique_object)
        if [PACKAGE, CASES[0]["test"]] not in manifest["suites"]["backend"]["required_tests"]:
            raise ValueError("missing-dsn selector must remain an actual mandatory backend PostgreSQL test")
        args.output_dir.mkdir(parents=True, exist_ok=True)
        for case in CASES:
            name, suite, pattern = case["name"], case["suite"], case["pattern"]
            log = (args.output_dir / f"{name}.jsonl").resolve()
            command = [args.go, "test", "-json", "-race", "-p", "2", "-count=1", "-timeout=180s", "-run", pattern, "./internal/store/postgres"]
            with log.open("x", encoding="utf-8") as output:
                run = subprocess.run(command,
                                     cwd=ROOT, env=env, stdout=output, stderr=subprocess.PIPE, text=True, timeout=240)
            write_exclusive(args.output_dir / f"{name}.stderr", run.stderr)
            write_exclusive(args.output_dir / f"{name}.go.exit", str(run.returncode) + "\n")
            checked = subprocess.run([sys.executable, "-B", str(ROOT / "scripts/check_go_test_evidence.py"), "--suite", suite,
                                      "--log", str(log), "--exit-code", str(run.returncode), "--source-sha", args.source_sha],
                                     cwd=ROOT, capture_output=True, text=True, timeout=30)
            write_exclusive(args.output_dir / f"{name}.gate.stdout", checked.stdout)
            write_exclusive(args.output_dir / f"{name}.gate.stderr", checked.stderr)
            write_exclusive(args.output_dir / f"{name}.gate.exit", str(checked.returncode) + "\n")
            if checked.stderr.strip():
                raise ValueError(f"{name}: unexpected strict-gate stderr")
            report = json.loads(checked.stdout, object_pairs_hook=unique_object)
            validation = validate_negative(case, log.read_text(), run.stderr, run.returncode, checked.returncode, report)
            with (args.output_dir / f"{name}-evidence.json").open("x", encoding="utf-8") as output:
                json.dump(report, output, indent=2, sort_keys=True)
                output.write("\n")
            with (args.output_dir / f"{name}-negative-validation.json").open("x", encoding="utf-8") as output:
                json.dump({**validation, "command": command, "source_sha": args.source_sha}, output, indent=2, sort_keys=True)
                output.write("\n")
            print(f"PASS negative {name}: go_exit={run.returncode} gate_exit=1 unknown=0 product_green=false")
    except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
        print(f"evidence integration FAILED: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
