"""Pure current-source negative-driver tests; no Go/PG process is launched."""
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).parents[2]


def module(name, file):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / file)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


driver = module("current_negative_driver", "test_go_evidence_integration.py")
gate = module("current_negative_gate", "check_go_test_evidence.py")


def event(action, test=None, **extra):
    value = {"Action": action, "Package": driver.PACKAGE, **extra}
    if test is not None:
        value["Test"] = test
    return value


def raw_case(case, package_terminal=None):
    events = [event("start")]
    if case["test"]:
        events += [event("run", case["test"]), event("output", case["test"],
                   Output="fixture.go:31: " + case["diagnostic"] + "\n"),
                   event(case["terminal"], case["test"])]
    events.append(event(package_terminal or ("fail" if case["go_exit"] else "pass")))
    return "\n".join(json.dumps(e) for e in events) + "\n"


class CurrentNegativeTests(unittest.TestCase):
    def validate(self, case, raw=None, go_exit=None, gate_exit=1, stderr="", mutate_report=None):
        raw = raw_case(case) if raw is None else raw
        go_exit = case["go_exit"] if go_exit is None else go_exit
        required, allowed = gate.load_suite(ROOT / "scripts/required_go_tests.json", case["suite"])
        report = gate.evaluate(raw.splitlines(), required, allowed, go_exit)
        report["process_exit_code"] = go_exit
        if mutate_report:
            mutate_report(report)
        return driver.validate_negative(case, raw, stderr, go_exit, gate_exit, report)

    def test_original_three_zero_exit_categories_remain_distinct(self):
        for case in driver.CASES[:3]:
            with self.subTest(case=case["name"]):
                result = self.validate(case)
                self.assertEqual(result["go_process_exit"], 0)
                self.assertEqual(result["gate_process_exit"], 1)
                self.assertEqual(result["unknown_failures"], 0)
                self.assertFalse(result["product_green"])

    def test_missing_dsn_selector_is_current_mandatory_legacy_pg(self):
        case = driver.CASES[0]
        required, _ = gate.load_suite(ROOT / "scripts/required_go_tests.json", "backend")
        self.assertIn((driver.PACKAGE, case["test"]), required)
        self.assertEqual(case["pattern"], "^TestR5SchemaInventoryAndRestart$")
        self.assertNotEqual(case["pattern"], ".")

    def test_current_missing_dsn_hardfail_is_expected_rejection_not_green(self):
        result = self.validate(driver.CASES[3])
        self.assertEqual(result["go_process_exit"], 1)
        self.assertEqual(result["status"], "expected_negative_rejection")
        self.assertFalse(result["product_green"])
        self.assertFalse(result["task_complete"])

    def test_exit_one_cannot_replace_any_original_zero_exit_case(self):
        for case in driver.CASES[:3]:
            with self.subTest(case=case["name"]), self.assertRaises(ValueError):
                self.validate(case, go_exit=1)

    def test_current_hardfail_exit_zero_is_not_reproduction(self):
        with self.assertRaises(ValueError):
            self.validate(driver.CASES[3], go_exit=0)

    def test_hardfail_marker_must_be_exact_single_source_diagnostic(self):
        case = driver.CASES[3]
        raw = raw_case(case)
        variants = [raw.replace(case["diagnostic"], "another failure"),
                    raw.replace(case["diagnostic"] + "\\n", "no marker\\n"),
                    raw.replace(case["diagnostic"] + "\\n", case["diagnostic"] + "\\nfixture.go:32: " + case["diagnostic"] + "\\n")]
        for value in variants:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.validate(case, raw=value)

    def test_panic_race_timeout_build_failure_or_stderr_are_unknown(self):
        case = driver.CASES[3]
        for poison in ["panic: broken", "WARNING: DATA RACE", "test timed out", "[build failed]", "context deadline exceeded"]:
            lines = raw_case(case).splitlines()
            lines.insert(-1, json.dumps(event("output", case["test"], Output=poison)))
            with self.subTest(poison=poison), self.assertRaises(ValueError):
                self.validate(case, raw="\n".join(lines))
        with self.assertRaises(ValueError):
            self.validate(case, stderr="compiler failed")

    def test_additional_test_or_assertion_or_scope_is_rejected(self):
        case = driver.CASES[3]
        for poison in [event("run", "TestOther"), event("output", case["test"], Output="fixture.go:55: another assertion\n"),
                       event("output", Output="fixture.go:31: " + case["diagnostic"] + "\n"),
                       {"Action": "start", "Package": "other/package"}, event("fail", FailedBuild="dependency")]:
            lines = raw_case(case).splitlines()
            lines.insert(-1, json.dumps(poison))
            with self.subTest(poison=poison), self.assertRaises(ValueError):
                self.validate(case, raw="\n".join(lines))

    def test_skip_pass_or_missing_terminal_cannot_replace_current_hardfail(self):
        case = driver.CASES[3]
        for action in ["skip", "pass", None]:
            events = [json.loads(line) for line in raw_case(case).splitlines()]
            if action is None:
                events = [e for e in events if not (e.get("Test") and e["Action"] == "fail")]
            else:
                next(e for e in events if e.get("Test") and e["Action"] == "fail")["Action"] = action
            with self.subTest(action=action), self.assertRaises(ValueError):
                self.validate(case, raw="\n".join(json.dumps(e) for e in events))

    def test_strict_gate_pass_or_unclassified_error_is_rejected(self):
        case = driver.CASES[3]
        for change in [lambda r: r.update(status="pass"), lambda r: r.update(tests_failed=0),
                       lambda r: r["errors"].append("unknown gate rejection"), lambda r: r.update(mandatory_missing_or_not_passed=0)]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.validate(case, mutate_report=change)
        with self.assertRaises(ValueError):
            self.validate(case, gate_exit=0)

    def test_zero_tests_can_use_empty_package_skip_but_no_test_skip(self):
        case = driver.CASES[1]
        self.validate(case, raw=raw_case(case, "skip"))
        events = [event("start"), event("run", "TestUnexpected"), event("skip", "TestUnexpected"), event("pass")]
        with self.assertRaises(ValueError):
            self.validate(case, raw="\n".join(json.dumps(e) for e in events))

    def test_duplicate_concatenated_or_malformed_logs_fail_closed(self):
        case = driver.CASES[3]
        for raw in [raw_case(case) * 2, "{", "[]", "", raw_case(case).replace('"Action": "run"', '"Action": "run", "Action": "run"')]:
            with self.subTest(raw=raw), self.assertRaises((ValueError, TypeError)):
                self.validate(case, raw=raw)


if __name__ == "__main__":
    unittest.main()
