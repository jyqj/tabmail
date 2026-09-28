#!/usr/bin/env python3
"""Run real Go processes proving that missing execution cannot pass the gate.

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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--source-sha", required=True)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.source_sha):
        parser.error("source-sha must be an exact 40-hex Git commit")
    env = dict(os.environ)
    env.pop("TABMAIL_TEST_DB_DSN", None)
    env.pop("TABMAIL_BROWSER_E2E", None)
    cases = [
        ("missing-dsn", "backend", "."),
        ("zero-tests", "backend", "^TestR5DefinitelyNoSuchTest$"),
        ("browser-disabled", "browser", "^TestR3BrowserJourney$"),
    ]
    try:
        args.output_dir.mkdir(parents=True, exist_ok=True)
        for name, suite, pattern in cases:
            log = (args.output_dir / f"{name}.jsonl").resolve()
            with log.open("x", encoding="utf-8") as output:
                run = subprocess.run([args.go, "test", "-json", "-race", "-p", "2", "-count=1", "-timeout=180s", "-run", pattern, "./internal/store/postgres"],
                                     cwd=ROOT, env=env, stdout=output, stderr=subprocess.PIPE, text=True, timeout=240)
            if run.returncode != 0:
                raise ValueError(f"{name}: Go process failed ({run.returncode}); this is not the intended zero-exit scenario")
            checked = subprocess.run([sys.executable, "-B", str(ROOT / "scripts/check_go_test_evidence.py"), "--suite", suite,
                                      "--log", str(log), "--exit-code", str(run.returncode), "--source-sha", args.source_sha],
                                     cwd=ROOT, capture_output=True, text=True, timeout=30)
            report = json.loads(checked.stdout)
            if checked.returncode != 1 or report.get("status") != "fail" or report.get("process_exit_code") != 0:
                raise ValueError(f"{name}: expected a structured gate rejection of a successful Go process")
            if report.get("tests_failed") != 0 or report.get("mandatory_missing_or_not_passed", 0) < 1:
                raise ValueError(f"{name}: rejection was not caused by missing mandatory execution")
            if any("malformed" in e or "transition" in e or "unknown Action" in e for e in report["errors"]):
                raise ValueError(f"{name}: malformed/unsupported log is not the intended rejection")
            if name == "zero-tests" and report["tests_started"] != 0:
                raise ValueError("zero-tests: the chosen filter unexpectedly selected tests")
            if name == "missing-dsn" and report["tests_skipped"] < 1:
                raise ValueError("missing-dsn: expected actual database tests to skip")
            if name == "browser-disabled" and (report["tests_started"], report["tests_skipped"]) != (1, 1):
                raise ValueError("browser-disabled: expected exactly the browser test to skip")
            with (args.output_dir / f"{name}-evidence.json").open("x", encoding="utf-8") as output:
                json.dump(report, output, indent=2, sort_keys=True)
                output.write("\n")
            print(f"PASS {name}: go_exit=0 gate_exit=1 started={report['tests_started']} skipped={report['tests_skipped']}")
    except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
        print(f"evidence integration FAILED: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
