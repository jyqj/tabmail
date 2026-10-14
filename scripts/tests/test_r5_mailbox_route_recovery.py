"""Counterexamples for the independent EXEC08 JSON/exit/run-boundary verifier.

These are synthetic verifier inputs, not UI executions or proof of the product
fix. Actual qualification requires the workflow's two real npm/Vitest runs.
"""
from __future__ import annotations

import copy
import importlib.util
import json
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location(
    "r5_mailbox_route_recovery",
    Path(__file__).resolve().parents[1] / "run_r5_mailbox_route_recovery.py")
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)
TEST_FILE = Path("/owned/candidate") / gate.TEST


def defect_message(case):
    if case in {"R01", "R02", "R03", "R04", "R05", "R06", "R14", "R15", "R16", "R17"}:
        return "AssertionError: expected [ {}, {} ] to have a length of +0 but got 2"
    if case == "R07":
        return "Error: expect(element).toBeDisabled()\nReceived element is not disabled:\n<button>Compose</button>"
    if case == "R13":
        return ("Error: expect(element).toHaveTextContent()\nExpected element to have text content:\n"
                "/requested mailbox.*unavailable/i\nReceived:\nNo messages")
    if case == "R18":
        return ('Error: expect(element).not.toBeInTheDocument()\n'
                'expected document not to contain element, found <input value="Saved cross-mailbox draft" /> instead')
    raise AssertionError(case)


def valid(baseline=False):
    assertions = []
    for title in gate.TITLES:
        full_name = gate.GROUP + " " + title
        failed = baseline and full_name in gate.NEGATIVE
        assertions.append({
            "ancestorTitles": [gate.GROUP], "fullName": full_name, "title": title,
            "status": "failed" if failed else "passed", "meta": {},
            "duration": 1, "failureMessages": [defect_message(title[:3])] if failed else [],
            "tags": [],
        })
    document = {
        "numTotalTests": 18, "numPassedTests": 5 if baseline else 18,
        "numFailedTests": 13 if baseline else 0, "numPendingTests": 0,
        "numTodoTests": 0, "numTotalTestSuites": 2,
        "numPassedTestSuites": 0 if baseline else 2,
        "numFailedTestSuites": 2 if baseline else 0, "numPendingTestSuites": 0,
        "success": not baseline, "startTime": 100,
        "snapshot": {"failure": False, "unmatched": 0, "unchecked": 0},
        "testResults": [{
            "name": str(TEST_FILE), "message": "", "status": "failed" if baseline else "passed",
            "startTime": 100, "endTime": 200, "assertionResults": assertions,
        }],
    }
    boundary = {
        "schema": 1, "nodeVersion": "22.20.0", "runStarts": 1, "runEnds": 1,
        "processTimeout": False, "reason": "failed" if baseline else "passed",
        "moduleFiles": [str(TEST_FILE)], "unhandledErrors": [], "scopeErrors": [],
    }
    return document, boundary


class MailboxRouteVerifierTests(unittest.TestCase):
    def reject(self, document, boundary, code=0, baseline=False):
        with self.assertRaises(ValueError):
            gate.verify(document, boundary, code, TEST_FILE, baseline)

    def test_accepts_complete_candidate(self):
        document, boundary = valid()
        result = gate.verify(document, boundary, 0, TEST_FILE, False)
        self.assertEqual((result["passed"], result["failed"], result["pending"]), (18, 0, 0))
        self.assertEqual(set(result["all_ids"]), gate.IDS)

    def test_accepts_only_fixed_baseline_business_failures(self):
        document, boundary = valid(True)
        result = gate.verify(document, boundary, 1, TEST_FILE, True)
        self.assertEqual((result["passed"], result["failed"]), (5, 13))
        self.assertEqual(set(result["negative_ids"]), gate.NEGATIVE)

    def test_rejects_exit_one_despite_success_true(self):
        document, boundary = valid()
        self.reject(document, boundary, code=1)

    def test_rejects_success_true_with_pending_assertion(self):
        document, boundary = valid()
        document["numPassedTests"], document["numPendingTests"] = 17, 1
        document["testResults"][0]["assertionResults"][-1]["status"] = "pending"
        self.reject(document, boundary)

    def test_rejects_reported_223_pass_six_pending_success_shape(self):
        # A synthetic model of the interrupted local report, never raw evidence.
        document, boundary = valid()
        document.update(numTotalTests=229, numPassedTests=223, numPendingTests=6, success=True)
        self.reject(document, boundary, code=1)

    def test_rejects_missing_assertion_even_with_original_summary(self):
        document, boundary = valid()
        document["testResults"][0]["assertionResults"].pop()
        self.reject(document, boundary)

    def test_rejects_duplicate_replacing_one_required_assertion(self):
        document, boundary = valid()
        assertions = document["testResults"][0]["assertionResults"]
        assertions[-1] = copy.deepcopy(assertions[0])
        self.reject(document, boundary)

    def test_rejects_extra_assertion(self):
        document, boundary = valid()
        document["testResults"][0]["assertionResults"].append(
            copy.deepcopy(document["testResults"][0]["assertionResults"][0]))
        self.reject(document, boundary)

    def test_rejects_unexpected_assertion_id(self):
        document, boundary = valid()
        document["testResults"][0]["assertionResults"][0]["fullName"] = "unrelated test"
        self.reject(document, boundary)

    def test_rejects_inconsistent_title_or_ancestor(self):
        for field, value in (("title", "different test"), ("ancestorTitles", ["different suite"])):
            with self.subTest(field=field):
                document, boundary = valid()
                document["testResults"][0]["assertionResults"][0][field] = value
                self.reject(document, boundary)

    def test_rejects_each_nonterminal_or_skipped_status(self):
        for status in ("pending", "skipped", "todo", "disabled", "running"):
            with self.subTest(status=status):
                document, boundary = valid()
                document["testResults"][0]["assertionResults"][0]["status"] = status
                self.reject(document, boundary)

    def test_rejects_passing_assertion_with_failure_message(self):
        document, boundary = valid()
        document["testResults"][0]["assertionResults"][0]["failureMessages"] = ["unrelated error"]
        self.reject(document, boundary)

    def test_rejects_passing_assertion_with_missing_failure_metadata(self):
        document, boundary = valid()
        document["testResults"][0]["assertionResults"][0].pop("failureMessages")
        self.reject(document, boundary)

    def test_rejects_wrong_baseline_failure_set_at_same_totals(self):
        document, boundary = valid(True)
        assertions = document["testResults"][0]["assertionResults"]
        assertions[0]["status"], assertions[0]["failureMessages"] = "passed", []
        assertions[7]["status"], assertions[7]["failureMessages"] = "failed", ["unrelated error"]
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_baseline_without_observed_defect(self):
        document, boundary = valid(True)
        document["testResults"][0]["assertionResults"][0]["failureMessages"] = ["AssertionError: unrelated"]
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_additional_error_on_already_failed_leaf(self):
        document, boundary = valid(True)
        document["testResults"][0]["assertionResults"][0]["failureMessages"].append(
            "Error: Unexpected synthetic request: GET /unrelated")
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_infrastructure_error_appended_to_defect_message(self):
        for error in ("Unexpected synthetic request: GET /unexpected", "Test timed out in 5000ms.",
                      "Hook timed out in 10000ms.", "TypeError: loader failed"):
            with self.subTest(error=error):
                document, boundary = valid(True)
                document["testResults"][0]["assertionResults"][0]["failureMessages"][0] += "\n" + error
                self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_missing_stale_editor_observation(self):
        document, boundary = valid(True)
        document["testResults"][0]["assertionResults"][-1]["failureMessages"] = [
            "expect(element).not.toBeInTheDocument(): expected document not to contain element"]
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_revocation_loader_error_instead_of_old_fallback(self):
        document, boundary = valid(True)
        document["testResults"][0]["assertionResults"][12]["failureMessages"] = [
            "Unable to find role=status; toHaveTextContent /requested mailbox.*unavailable/i"]
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_extra_file(self):
        document, boundary = valid()
        document["testResults"].append({
            "name": "/owned/other.test.tsx", "status": "passed", "message": "", "assertionResults": []})
        self.reject(document, boundary)

    def test_rejects_missing_report_or_boundary(self):
        document, boundary = valid()
        self.reject({}, boundary)
        self.reject(document, {})
        self.reject(document, None)

    def test_rejects_wrong_file_path(self):
        document, boundary = valid()
        document["testResults"][0]["name"] = "features/mail/workspace-mailbox-route-recovery.test.tsx"
        self.reject(document, boundary)

    def test_rejects_file_collection_error(self):
        document, boundary = valid(True)
        document["testResults"][0]["message"] = "Error: afterAll hook failed"
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_error_attached_to_already_failed_suite_scope(self):
        document, boundary = valid(True)
        boundary["scopeErrors"] = [{"scope": gate.GROUP, "message": "extra afterAll failure"}]
        self.reject(document, boundary, code=1, baseline=True)

    def test_rejects_unhandled_error_despite_expected_assertions(self):
        for baseline in (False, True):
            with self.subTest(baseline=baseline):
                document, boundary = valid(baseline)
                boundary["unhandledErrors"] = [{"message": "unhandled promise rejection"}]
                self.reject(document, boundary, code=1 if baseline else 0, baseline=baseline)

    def test_rejects_extra_runtime_error_fields(self):
        for scope in ("top", "file", "assertion"):
            with self.subTest(scope=scope):
                document, boundary = valid()
                target = document if scope == "top" else document["testResults"][0]
                if scope == "assertion":
                    target = target["assertionResults"][0]
                target["errors"] = ["unrelated runtime failure"]
                self.reject(document, boundary)

    def test_rejects_summary_counter_disagreement(self):
        for key in ("numTotalTests", "numPassedTests", "numFailedTests", "numPendingTests",
                    "numTodoTests", "numTotalTestSuites", "numPassedTestSuites",
                    "numFailedTestSuites", "numPendingTestSuites"):
            with self.subTest(key=key):
                document, boundary = valid()
                document[key] += 1
                self.reject(document, boundary)

    def test_rejects_boolean_disguised_as_integer_count_or_exit(self):
        document, boundary = valid()
        document["numFailedTests"] = False
        self.reject(document, boundary)
        document, boundary = valid()
        self.reject(document, boundary, code=False)

    def test_rejects_summary_success_disagreement(self):
        for baseline in (False, True):
            with self.subTest(baseline=baseline):
                document, boundary = valid(baseline)
                document["success"] = baseline
                self.reject(document, boundary, code=1 if baseline else 0, baseline=baseline)

    def test_rejects_wrong_baseline_exit(self):
        document, boundary = valid(True)
        for code in (0, 2, 124, -9):
            with self.subTest(code=code):
                self.reject(document, boundary, code=code, baseline=True)

    def test_rejects_incomplete_duplicate_or_interrupted_run(self):
        for key, value in (("runStarts", 0), ("runStarts", 2), ("runEnds", 0),
                           ("runEnds", 2), ("reason", "interrupted"),
                           ("processTimeout", True), ("schema", 2)):
            with self.subTest(key=key, value=value):
                document, boundary = valid()
                boundary[key] = value
                self.reject(document, boundary)

    def test_rejects_wrong_node_or_extra_module(self):
        document, boundary = valid()
        boundary["nodeVersion"] = "24.0.0"
        self.reject(document, boundary)
        document, boundary = valid()
        boundary["moduleFiles"].append("/owned/other.test.tsx")
        self.reject(document, boundary)

    def test_rejects_snapshot_failure(self):
        document, boundary = valid()
        document["snapshot"]["failure"] = True
        self.reject(document, boundary)

    def test_parser_rejects_duplicate_object_keys(self):
        with self.assertRaises(ValueError):
            gate.parse_json('{"success":true,"success":false}')

    def test_parser_rejects_non_finite_numbers(self):
        for value in ("NaN", "Infinity", "-Infinity"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                gate.parse_json('{"duration":' + value + "}")

    def test_parser_preserves_valid_report(self):
        document, _ = valid()
        self.assertEqual(gate.parse_json(json.dumps(document)), document)


if __name__ == "__main__":
    unittest.main()
