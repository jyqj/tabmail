"""Catalog and scan-boundary regressions; source AST cases run under Node."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("check_i18n_keys", Path(__file__).parents[1] / "check_i18n_keys.py")
check = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(check)


class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        (self.root / "web/locales").mkdir(parents=True)
        for directory in check.SOURCE_DIRS:
            (self.root / "web" / directory).mkdir()
        self.write_catalog("zh", {"ok": "正确"})
        self.write_catalog("en", {"ok": "OK"})
        (self.root / "web/features/view.tsx").write_text('t("ok")', encoding="utf-8")

    def write_catalog(self, locale, data):
        (self.root / "web/locales" / f"{locale}.json").write_text(json.dumps(data), encoding="utf-8")

    def test_valid_catalog(self):
        self.assertEqual(check.catalog_keys(self.root, "zh"), {"ok"})

    def test_missing_catalog(self):
        (self.root / "web/locales/zh.json").unlink()
        with self.assertRaises(check.CheckError): check.catalog_keys(self.root, "zh")

    def test_malformed_and_duplicate_catalog(self):
        for raw in ['{', '{"ok":"one","ok":"two"}']:
            with self.subTest(raw=raw):
                (self.root / "web/locales/zh.json").write_text(raw)
                with self.assertRaises(check.CheckError): check.catalog_keys(self.root, "zh")

    def test_empty_wrong_type_and_bad_values(self):
        for data in [{}, [], {"ok": None}, {"ok": 1}, {"ok": {}}, {"ok": " "}, {"": "text"}]:
            with self.subTest(data=data):
                self.write_catalog("zh", data)
                with self.assertRaises(check.CheckError): check.catalog_keys(self.root, "zh")

    def test_strict_utf8(self):
        (self.root / "web/locales/zh.json").write_bytes(b'\xff')
        with self.assertRaises(check.CheckError): check.catalog_keys(self.root, "zh")

    def test_catalog_mismatch_precedes_collector(self):
        self.write_catalog("en", {"other": "Other"})
        with patch.object(check, "collect_keys") as collector:
            with self.assertRaises(check.CheckError): check.validate(self.root)
            collector.assert_not_called()

    def test_scan_includes_features_and_hooks_not_generated_or_tests(self):
        (self.root / "web/hooks/use-view.ts").write_text('t("ok")')
        (self.root / "web/features/view.test.tsx").write_text('t("test.only")')
        (self.root / "web/features/.next").mkdir()
        (self.root / "web/features/.next/built.ts").write_text('t("build.only")')
        found = [p.relative_to(self.root).as_posix() for p in check.source_files(self.root)]
        self.assertEqual(found, ["web/features/view.tsx", "web/hooks/use-view.ts"])

    def test_missing_source_directory_fails(self):
        (self.root / "web/hooks").rmdir()
        with self.assertRaises(check.CheckError): check.source_files(self.root)

    def test_empty_scan_fails(self):
        (self.root / "web/features/view.tsx").unlink()
        with self.assertRaises(check.CheckError): check.source_files(self.root)

    def test_missing_features_key_fails(self):
        result = {"files": 1, "keys": [{"key": "feature.missing", "location": "web/features/view.tsx:1"}], "dynamic_calls": 0, "inline_calls": 0}
        with patch.object(check, "collect_keys", return_value=result):
            with self.assertRaisesRegex(check.CheckError, "feature.missing"): check.validate(self.root)

    def test_valid_end_to_end_contract(self):
        result = {"files": 1, "keys": [{"key": "ok", "location": "web/features/view.tsx:1"}], "dynamic_calls": 0, "inline_calls": 0}
        with patch.object(check, "collect_keys", return_value=result):
            self.assertEqual(check.validate(self.root)["literal_keys"], 1)

    def test_missing_node_fails(self):
        with self.assertRaises(check.CheckError):
            check.collect_keys(self.root, check.source_files(self.root), "/nonexistent/r5-node")

    def test_invalid_or_empty_collector_output_fails(self):
        import subprocess
        for output in ["not json", "{}", '{"files":1,"keys":[]}', '{"files":0,"keys":[{"key":"ok"}]}']:
            with self.subTest(output=output), patch.object(check.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, output, "")):
                with self.assertRaises(check.CheckError):
                    check.collect_keys(self.root, check.source_files(self.root), "node")


if __name__ == "__main__": unittest.main()
