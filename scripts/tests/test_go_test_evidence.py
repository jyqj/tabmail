"""Counterexamples for missing, skipped, malformed or overwritten evidence."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("gate", Path(__file__).parents[1] / "check_go_test_evidence.py")
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)
P = "tabmail/internal/store/postgres"
REQUIRED = {(P, "TestDatabase")}


def event(action, test=None, package=P, **extra):
    value = {"Action": action, "Package": package, **extra}
    if test is not None: value["Test"] = test
    return json.dumps(value) + "\n"


def happy():
    return [event("start"), event("run", "TestDatabase"), event("pass", "TestDatabase"), event("pass")]


class EvidenceTests(unittest.TestCase):
    def check(self, lines, good=False, code=0, allowed=None):
        result = gate.evaluate(lines, REQUIRED, allowed or {}, code)
        self.assertEqual(result["status"], "pass" if good else "fail", result)
        return result

    def test_real_lifecycle_passes(self): self.check(happy(), good=True)
    def test_empty_log_fails(self): self.check([])
    def test_package_pass_is_not_a_test(self): self.check([event("start"), event("pass")])
    def test_output_pass_text_is_not_a_test(self):
        self.check([event("start"), event("output", Output="--- PASS: TestDatabase\nPASS\n"), event("pass")])
    def test_skip_is_not_pass(self):
        self.check([event("start"), event("run", "TestDatabase"), event("skip", "TestDatabase"), event("pass")])
    def test_nonzero_exit_fails_even_with_green_events(self): self.check(happy(), code=1)
    def test_pass_without_run_fails(self): self.check([event("start"), event("pass", "TestDatabase"), event("pass")])
    def test_missing_package_start_fails(self): self.check(happy()[1:])
    def test_missing_test_completion_fails(self): self.check(happy()[:2] + [event("pass")])
    def test_missing_package_completion_fails(self): self.check(happy()[:-1])
    def test_corrupted_or_non_object_events_fail(self):
        for line in ["{", "[]", "null", "\n", '{"Action":"pass","Action":"run","Package":"x"}']:
            with self.subTest(line=line): self.check(happy() + [line])
    def test_unknown_action_fails(self): self.check(happy() + [event("unknown")])
    def test_invalid_field_types_fail(self):
        for line in [event([], "TestDatabase"), event("run", 7), event("run", "TestDatabase", package=8), event("output")]:
            with self.subTest(line=line): self.check(happy() + [line])
    def test_failed_then_passed_cannot_overwrite_failure(self):
        self.check([event("start"), event("run", "TestDatabase"), event("fail", "TestDatabase"), event("pass", "TestDatabase"), event("pass")])
    def test_concatenated_runs_fail(self): self.check(happy() + happy())
    def test_duplicate_test_run_fails(self): self.check(happy()[:2] + [event("run", "TestDatabase")] + happy()[2:])
    def test_package_failure_fails(self): self.check(happy()[:-1] + [event("fail")])
    def test_build_failure_fails(self): self.check(happy()[:-1] + [event("fail", FailedBuild="dependency")])
    def test_wrong_package_does_not_satisfy_identity(self):
        self.check([line.replace(P, "other/package") for line in happy()])
    def test_unexpected_skip_fails_even_if_required_passes(self):
        self.check(happy()[:-1] + [event("run", "TestOther"), event("skip", "TestOther"), event("pass")])
    def test_documented_exact_optional_skip_is_reported(self):
        r = self.check(happy()[:-1] + [event("run", "TestBrowser"), event("skip", "TestBrowser"), event("pass")], good=True, allowed={(P, "TestBrowser"): "separate job"})
        self.assertEqual(r["tests_skipped"], 1)
        self.assertEqual(len(r["allowed_skips_observed"]), 1)
    def test_skipped_child_is_not_hidden_by_parent_pass(self):
        self.check(happy()[:2] + [event("run", "TestDatabase/realDB"), event("skip", "TestDatabase/realDB")] + happy()[2:])
    def test_parallel_pause_and_resume(self):
        self.check(happy()[:2] + [event("pause", "TestDatabase"), event("cont", "TestDatabase")] + happy()[2:], good=True)
    def test_interleaved_packages_and_subtests(self):
        self.check(happy()[:2] + [event("start", package="other"), event("run", "TestExtra", package="other"), event("run", "TestDatabase/child"), event("pass", "TestExtra", package="other"), event("pass", package="other"), event("pass", "TestDatabase/child")] + happy()[2:], good=True)
    def test_empty_package_is_allowed_but_not_substitute_for_required(self):
        self.check(happy() + [event("start", package="empty"), event("skip", package="empty")], good=True)


class ManifestTests(unittest.TestCase):
    def manifest(self):
        return {"version": 1, "suites": {"backend": {"required_tests": [[P, "TestDatabase"]], "allowed_skips": []}}}

    def load(self, value, suite="backend"):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "manifest.json"
            path.write_text(json.dumps(value), encoding="utf-8")
            return gate.load_suite(path, suite)

    def test_valid_manifest(self): self.assertEqual(self.load(self.manifest())[0], REQUIRED)
    def test_invalid_manifest_version_fails(self):
        for version in [True, False, None, "1", 2]:
            with self.subTest(version=version):
                value = self.manifest(); value["version"] = version
                with self.assertRaises(gate.EvidenceError): self.load(value)
    def test_missing_suite_fails(self):
        with self.assertRaises(gate.EvidenceError): self.load(self.manifest(), "typo")
    def test_empty_or_duplicate_requirements_fail(self):
        for required in [[], [[P,"TestDatabase"],[P,"TestDatabase"]], "not-a-list", [[P,"Test*"]]]:
            with self.subTest(required=required):
                value = self.manifest(); value["suites"]["backend"]["required_tests"] = required
                with self.assertRaises(gate.EvidenceError): self.load(value)
    def test_optional_overlap_or_no_reason_fails(self):
        for entry in [{"identity":[P,"TestDatabase"],"reason":"overlap"}, {"identity":[P,"TestOther"],"reason":""}]:
            with self.subTest(entry=entry):
                value=self.manifest(); value["suites"]["backend"]["allowed_skips"]=[entry]
                with self.assertRaises(gate.EvidenceError): self.load(value)


if __name__ == "__main__": unittest.main()
