"""Qualify the new EXEC08 mailbox-route fixture on two exact source trees.

This is a new 251-line fixture at a new path. It does not reuse or claim the
lost 181-line fixture's identity or its historical execution results. The fixed
R2 baseline gains only this frozen file; the candidate runs the identical
bytes. Vitest assertions, exit status and an independent run-end reporter
must agree. No pending, skipped, unrelated or infrastructure error is a pass.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

BASELINE = "ff43326e81f262daaac7b3dfd8b3047a8f51743b"
TEST = "web/features/mail/workspace-mailbox-route-recovery.test.tsx"
TEST_HASH = "2a72bb78cc5f9e82d037a0358875c2d0c433d8cd956e0e7801795756c88e9c66"
GROUP = "explicit mailbox route recovery"
TITLES = (
    "R01 keeps an unavailable inbox route unresolved",
    "R02 keeps an unavailable sent route unresolved",
    "R03 keeps an unavailable archive route unresolved",
    "R04 keeps an unavailable trash route unresolved",
    "R05 rejects an explicit mailbox with no read or send rights",
    "R06 distinguishes an empty explicit mailbox from an absent parameter",
    "R07 prevents Compose from silently choosing another mailbox",
    "R08 retains the ordinary default when no mailbox parameter exists",
    "R09 resolves a valid explicit mailbox instead of the first one",
    "R10 keeps a selected send-only identity usable for Compose",
    "R11 retains an authorized sender for a selected read-only mailbox",
    "R12 does not report permanent unavailability during the initial read",
    "R13 closes a revoked mailbox and its stream without a fallback read",
    "R14 resolves the requested mailbox after an explicit refresh restores access",
    "R15 reads another mailbox only after explicit selection and clears stale detail and page",
    "R16 preserves cross-mailbox drafts and explicitly opening one",
    "R17 preserves cross-mailbox delivery status without a fallback stream",
    "R18 retires pending editor opening between two unavailable mailbox routes",
)
IDS = {GROUP + " " + title for title in TITLES}
POSITIVE = {GROUP + " " + title for title in TITLES[7:12]}
NEGATIVE = IDS - POSITIVE
ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
FORBIDDEN_FAILURE = re.compile(
    r"Unexpected synthetic request|Test timed out|Hook timed out|"
    r"Unhandled (?:Error|Rejection|Exception)|Failed to load|"
    r"Cannot find (?:module|package)|Worker exited|"
    r"ReferenceError|SyntaxError|TypeError", re.IGNORECASE)

# Vitest 4.1.11's JSON reporter omits unhandledErrors and can set success=true
# while tests are pending. Its official onTestRunEnd hook supplies the missing
# execution boundary. This module lives in the external evidence directory;
# the baseline overlay still contains only TEST.
REPORTER = r"""
import { writeFileSync } from "node:fs";
export default class ExecutionBoundaryReporter {
  constructor() {
    this.data = {
      schema: 1, nodeVersion: process.versions.node, runStarts: 0, runEnds: 0,
      processTimeout: false, reason: null, moduleFiles: [],
      unhandledErrors: [], scopeErrors: [],
    };
  }
  write() { writeFileSync(outputFile, JSON.stringify(this.data, null, 2) + "\n"); }
  onTestRunStart() { this.data.runStarts += 1; this.write(); }
  onProcessTimeout() { this.data.processTimeout = true; this.write(); }
  onTestRunEnd(modules, errors, reason) {
    this.data.runEnds += 1;
    this.data.reason = reason;
    this.data.moduleFiles = modules.map(module => module.task.filepath);
    const describeError = error => ({
      name: String(error?.name ?? ""), message: String(error?.message ?? error),
      stack: String(error?.stack ?? ""),
    });
    this.data.unhandledErrors = errors.map(describeError);
    this.data.scopeErrors = [];
    const visit = task => {
      if (task.type !== "suite") return;
      for (const error of task.result?.errors ?? []) {
        this.data.scopeErrors.push({ scope: task.name, ...describeError(error) });
      }
      for (const child of task.tasks) visit(child);
    };
    for (const module of modules) visit(module.task);
    this.write();
  }
}
"""


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON object key: " + key)
        result[key] = value
    return result


def invalid_constant(value):
    raise ValueError("non-finite JSON number: " + value)


def parse_json(text):
    return json.loads(text, object_pairs_hook=unique_object,
                      parse_constant=invalid_constant)


def load_json(path):
    return parse_json(path.read_text(encoding="utf-8"))


def verify_failure(full_name, message):
    require(isinstance(message, str) and bool(message), "missing defect assertion")
    message = ANSI.sub("", message)
    require(not FORBIDDEN_FAILURE.search(message), "unrelated or infrastructure assertion")
    case = full_name.removeprefix(GROUP + " ").split(" ", 1)[0]
    if case in {"R01", "R02", "R03", "R04", "R05", "R06", "R14", "R15", "R16", "R17"}:
        require(bool(re.search(r"to have a length of \+?0 but got [1-9][0-9]*\b", message)),
                "baseline did not observe an unintended mailbox content read")
    elif case == "R07":
        require("toBeDisabled" in message and "is not disabled" in message,
                "baseline did not observe enabled fallback Compose")
    elif case == "R13":
        require("toHaveTextContent" in message and "requested mailbox" in message
                and "No messages" in message,
                "baseline did not observe fallback contents after revocation")
    elif case == "R18":
        require("not.toBeInTheDocument" in message
                and "expected document not to contain element" in message
                and "Saved cross-mailbox draft" in message,
                "baseline did not observe the stale draft editor")
    else:
        raise ValueError("unknown permitted baseline defect")


def verify(document, boundary, exit_code, test_file, baseline):
    require(type(baseline) is bool, "explicit source mode required")
    require(type(exit_code) is int and exit_code == (1 if baseline else 0),
            "process exit does not match the required baseline/candidate outcome")
    require(isinstance(document, dict), "missing Vitest JSON object")
    counts = {
        "numTotalTests": 18, "numPassedTests": 5 if baseline else 18,
        "numFailedTests": 13 if baseline else 0, "numPendingTests": 0,
        "numTodoTests": 0, "numTotalTestSuites": 2,
        "numPassedTestSuites": 0 if baseline else 2,
        "numFailedTestSuites": 2 if baseline else 0, "numPendingTestSuites": 0,
    }
    allowed = set(counts) | {"startTime", "success", "testResults", "snapshot", "coverageMap"}
    require(set(document) <= allowed, "unexpected top-level Vitest fields or errors")
    for key, expected in counts.items():
        require(type(document.get(key)) is int and document[key] == expected,
                "Vitest count does not match complete execution: " + key)
    require(document.get("success") is (not baseline), "contradictory Vitest success flag")
    snapshot = document.get("snapshot")
    require(isinstance(snapshot, dict), "missing snapshot execution metadata")
    require(not snapshot.get("failure") and not snapshot.get("unmatched")
            and not snapshot.get("unchecked"), "unexpected snapshot failure")
    files = document.get("testResults")
    require(isinstance(files, list) and len(files) == 1, "missing or extra Vitest test file")
    file = files[0]
    require(isinstance(file, dict), "invalid Vitest file result")
    require(file.get("name") == str(test_file), "unexpected test file path")
    require(file.get("status") == ("failed" if baseline else "passed"),
            "contradictory Vitest file status")
    require(file.get("message") == "", "file-level collection or hook error")
    require(set(file) <= {"message", "name", "status", "startTime", "endTime", "assertionResults"},
            "unexpected file-level error fields")
    assertions = file.get("assertionResults")
    require(isinstance(assertions, list) and len(assertions) == 18,
            "missing or extra required assertions")
    names = []
    for assertion in assertions:
        require(isinstance(assertion, dict), "invalid assertion result")
        name = assertion.get("fullName")
        require(isinstance(name, str) and name in IDS, "unexpected assertion ID")
        require(name not in names, "duplicate assertion ID")
        names.append(name)
        title = name.removeprefix(GROUP + " ")
        require(assertion.get("title") == title and assertion.get("ancestorTitles") == [GROUP],
                "assertion ID does not match its suite/title")
        require(set(assertion) <= {"ancestorTitles", "fullName", "status", "title",
                                  "meta", "duration", "failureMessages", "location", "tags"},
                "unexpected assertion error fields")
        expected_status = "failed" if baseline and name in NEGATIVE else "passed"
        require(assertion.get("status") == expected_status,
                "unfinished, pending, skipped, unrelated or incorrect assertion: " + name)
        messages = assertion.get("failureMessages")
        require(isinstance(messages, list), "missing assertion failure metadata")
        if expected_status == "passed":
            require(messages == [], "passing assertion retains an error")
        else:
            require(len(messages) == 1, "baseline has missing or additional assertion errors")
            verify_failure(name, messages[0])
    require(set(names) == IDS, "required assertion ID set differs")
    expected_boundary_keys = {"schema", "nodeVersion", "runStarts", "runEnds",
                              "processTimeout", "reason", "moduleFiles",
                              "unhandledErrors", "scopeErrors"}
    require(isinstance(boundary, dict) and set(boundary) == expected_boundary_keys,
            "missing or malformed independent execution boundary")
    require(type(boundary["schema"]) is int and boundary["schema"] == 1
            and type(boundary["runStarts"]) is int and boundary["runStarts"] == 1
            and type(boundary["runEnds"]) is int and boundary["runEnds"] == 1,
            "missing, duplicate or incomplete test run")
    require(isinstance(boundary["nodeVersion"], str)
            and bool(re.fullmatch(r"22\.\d+\.\d+", boundary["nodeVersion"])),
            "regression did not execute under Node 22")
    require(boundary["processTimeout"] is False, "Vitest process timeout")
    require(boundary["reason"] == ("failed" if baseline else "passed"),
            "run interrupted or independent outcome disagrees")
    require(boundary["moduleFiles"] == [str(test_file)], "extra or missing execution module")
    require(boundary["unhandledErrors"] == [], "unhandled execution errors")
    require(boundary["scopeErrors"] == [], "collection or suite hook errors")
    return {"leaf_count": 18, "passed": 5 if baseline else 18,
            "failed": 13 if baseline else 0, "pending": 0, "skipped": 0,
            "exit_code": exit_code, "positive_ids": sorted(POSITIVE),
            "negative_ids": sorted(NEGATIVE), "all_ids": sorted(names)}


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args],
                                   text=True, timeout=30).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def identity(root):
    return {"commit": git(root, "rev-parse", "HEAD"),
            "tree": git(root, "rev-parse", "HEAD^{tree}"),
            "status": git(root, "status", "--porcelain=v1", "--untracked-files=all"),
            "test_sha256": digest(root / TEST),
            "package_lock_sha256": digest(root / "web/package-lock.json")}


def validate_identity(row, expected_commit=None):
    require(row["status"] == "", "regression source must be clean")
    require(row["test_sha256"] == TEST_HASH, "frozen new regression bytes differ")
    if expected_commit is not None:
        require(row["commit"] == expected_commit, "candidate commit differs from requested source")


def validate_baseline(root):
    require(git(root, "rev-parse", "HEAD^") == BASELINE, "wrong baseline parent")
    require(git(root, "rev-list", "--parents", "-n", "1", "HEAD").split() ==
            [git(root, "rev-parse", "HEAD"), BASELINE], "baseline must have one fixed parent")
    require(git(root, "diff", "--name-status", BASELINE, "HEAD") == "A\t" + TEST,
            "baseline overlay changed more than the one frozen regression")


def overlay_baseline(baseline, candidate, candidate_sha):
    validate_identity(identity(candidate), candidate_sha)
    require(git(baseline, "status", "--porcelain=v1", "--untracked-files=all") == "",
            "dirty baseline before overlay")
    require(git(baseline, "rev-parse", "HEAD") == BASELINE, "wrong public baseline before overlay")
    require(not git(baseline, "ls-tree", "HEAD", "--", TEST), "test already exists in baseline")
    (baseline / TEST).write_bytes((candidate / TEST).read_bytes())
    git(baseline, "add", "--", TEST)
    git(baseline, "-c", "user.name=Owned regression fixture",
        "-c", "user.email=fixture@example.invalid", "commit", "--no-gpg-sign",
        "-m", "Overlay identical new EXEC08 mailbox-route regression on public R2")
    validate_baseline(baseline)
    row = identity(baseline)
    validate_identity(row)
    return row


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def execute(command, root, output, prefix, timeout):
    stdout, stderr = output / (prefix + ".stdout"), output / (prefix + ".stderr")
    row = {"command": command, "cwd": str(root), "timeout_seconds": timeout,
           "started_at": time.time(), "returncode": None, "timed_out": False}
    environment = dict(os.environ, CI="true", NO_COLOR="1")
    with stdout.open("wb") as out, stderr.open("wb") as err:
        try:
            process = subprocess.Popen(command, cwd=root, stdout=out, stderr=err,
                                       env=environment, start_new_session=True)
            try:
                process.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                row["timed_out"] = True
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
            row["returncode"] = process.returncode
        except Exception as error:
            row["start_or_wait_error"] = {"type": type(error).__name__, "message": str(error)}
    row.update({"finished_at": time.time(), "stdout_sha256": digest(stdout),
                "stderr_sha256": digest(stderr)})
    write_json(output / (prefix + ".execution.json"), row)
    return row


def successful(row):
    return row["returncode"] == 0 and not row["timed_out"] and "start_or_wait_error" not in row


def run(root, output, baseline, candidate_sha):
    output.mkdir(mode=0o700)
    row = {"mode": "baseline" if baseline else "candidate", "root": str(root),
           "expected_baseline": BASELINE, "expected_candidate": candidate_sha,
           "test_sha256": TEST_HASH, "python": sys.version,
           "runner_sha256": digest(Path(__file__)), "before": None, "after": None}
    failure = None
    try:
        row["before"] = identity(root)
        validate_identity(row["before"], None if baseline else candidate_sha)
        if baseline:
            validate_baseline(root)
        row["node"] = execute(["node", "--version"], root / "web", output, "node-version", 30)
        require(successful(row["node"]), "Node version command failed")
        node_version = (output / "node-version.stdout").read_text().strip()
        require(bool(re.fullmatch(r"v22\.\d+\.\d+", node_version)), "Node 22 is required")
        row["npm"] = execute(["npm", "--version"], root / "web", output, "npm-version", 30)
        require(successful(row["npm"]), "npm version command failed")
        row["install"] = execute(["npm", "ci"], root / "web", output, "npm-ci", 300)
        require(successful(row["install"]), "npm ci did not complete successfully")
        reporter = output / "execution-boundary-reporter.mjs"
        reporter.write_text("const outputFile = " + json.dumps(str(output / "run-status.json"))
                            + ";\n" + REPORTER, encoding="utf-8")
        row["reporter_sha256"] = digest(reporter)
        command = ["node", str(root / "web/node_modules/vitest/vitest.mjs"), "run",
                   TEST.removeprefix("web/"), "--maxWorkers=1", "--no-file-parallelism",
                   "--reporter=json", "--reporter=" + str(reporter),
                   "--outputFile=" + str(output / "vitest.json")]
        row["test"] = execute(command, root / "web", output, "vitest", 120)
        require(not row["test"]["timed_out"] and "start_or_wait_error" not in row["test"],
                "Vitest watchdog, startup or wait failure")
        require(digest(reporter) == row["reporter_sha256"], "execution reporter changed")
        row["results"] = verify(load_json(output / "vitest.json"),
                                load_json(output / "run-status.json"),
                                row["test"]["returncode"], root / TEST, baseline)
    except Exception as error:
        failure = error
        row["error"] = {"type": type(error).__name__, "message": str(error)}
    finally:
        try:
            row["after"] = identity(root)
            validate_identity(row["after"], None if baseline else candidate_sha)
            if baseline:
                validate_baseline(root)
            require(row["before"] == row["after"], "source changed during installation or execution")
        except Exception as error:
            row["identity_error"] = {"type": type(error).__name__, "message": str(error)}
            if failure is None:
                failure = error
        row["evidence_sha256"] = {
            file.name: digest(file) for file in sorted(output.iterdir()) if file.is_file()
        }
        write_json(output / "execution.json", row)
    if failure is not None:
        raise ValueError(row.get("error", row.get("identity_error")))
    write_json(output / "verified.json", row)
    return row


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--candidate-sha", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    baseline, candidate, output = args.baseline.resolve(), args.candidate.resolve(), args.output.resolve()
    require(bool(re.fullmatch(r"[0-9a-f]{40}", args.candidate_sha)), "full candidate SHA required")
    require(not baseline.is_relative_to(candidate) and not candidate.is_relative_to(baseline),
            "two separate source directories required")
    require(all(not output.is_relative_to(root) and not root.is_relative_to(output)
                for root in (baseline, candidate)), "external evidence directory required")
    output.mkdir(mode=0o700)
    summary = {"baseline": BASELINE, "candidate": args.candidate_sha,
               "test": TEST, "test_sha256": TEST_HASH, "results": {}, "errors": {}}
    try:
        summary["overlay"] = overlay_baseline(baseline, candidate, args.candidate_sha)
    except Exception as error:
        summary["errors"]["overlay"] = {"type": type(error).__name__, "message": str(error)}
    for label, root in (("baseline", baseline), ("candidate", candidate)):
        try:
            summary["results"][label] = run(root, output / label, label == "baseline", args.candidate_sha)
        except Exception as error:
            summary["errors"][label] = {"type": type(error).__name__, "message": str(error)}
    write_json(output / "summary.json", summary)
    print(json.dumps({"passed": not summary["errors"],
                      "results": {name: row["results"] for name, row in summary["results"].items()},
                      "errors": summary["errors"]}, sort_keys=True))
    return 1 if summary["errors"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
