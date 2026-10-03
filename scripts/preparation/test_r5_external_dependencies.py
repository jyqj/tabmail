"""Synthetic preparation negative controls; no product/fixture execution."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("external", Path(__file__).with_name("r5_external_dependencies.py"))
external = importlib.util.module_from_spec(spec)
spec.loader.exec_module(external)


class PreparationControls(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.dependencies = self.root / "deps"
        (self.source / "web").mkdir(parents=True)
        package = self.dependencies / "node_modules" / "tool"
        package.mkdir(parents=True)
        (package / "package.json").write_text(json.dumps(dict(name="tool", version="1.0.0", bin={"tool": "cli.js"})))
        (package / "cli.js").write_text("owned synthetic bytes; never executed")
        (package.parent / ".bin").mkdir()
        self.link = package.parent / ".bin" / "tool"
        self.link.symlink_to("../tool/cli.js")
        self.lock = dict(lockfileVersion=3, packages={"": {}, "node_modules/tool": dict(version="1.0.0", resolved="https://registry.npmjs.org/tool/-/tool-1.0.0.tgz", integrity="sha512-candidate")})
        for directory in [self.source / "web", self.dependencies]:
            (directory / "package.json").write_text('{"private":true}')
            (directory / "package-lock.json").write_text(json.dumps(self.lock))

    def observe(self):
        return external.observe(self.source, self.dependencies)

    def test_control_exact_bin_and_inventory(self):
        receipt = self.observe()
        external.validate(receipt, self.source, self.dependencies)
        self.assertFalse(receipt["actual_component_started"])
        self.assertEqual(receipt["status"], "PREPARED_CANDIDATE_UNADOPTED")

    def test_lock_identity_drift(self):
        (self.dependencies / "package-lock.json").write_text("{}")
        with self.assertRaisesRegex(ValueError, "identity differs"):
            self.observe()

    def test_nonofficial_registry(self):
        bad = copy.deepcopy(self.lock)
        bad["packages"]["node_modules/tool"]["resolved"] = "https://example.invalid/tool.tgz"
        with self.assertRaises(ValueError):
            external.check_lock(json.dumps(bad).encode())

    def test_unbound_bundle(self):
        bad = copy.deepcopy(self.lock)
        bad["packages"]["node_modules/tool"] = dict(inBundle=True)
        with self.assertRaisesRegex(ValueError, "unbound bundled"):
            external.check_lock(json.dumps(bad).encode())

    def test_escaping_link(self):
        self.link.unlink()
        self.link.symlink_to(self.source / "web" / "package.json")
        with self.assertRaisesRegex(ValueError, "escaping"):
            self.observe()

    def test_undeclared_bin(self):
        self.link.rename(self.link.with_name("unknown"))
        with self.assertRaisesRegex(ValueError, "undeclared"):
            self.observe()

    def test_non_bin_link(self):
        (self.dependencies / "node_modules" / "linked-code").symlink_to("tool/cli.js")
        with self.assertRaisesRegex(ValueError, "undeclared"):
            self.observe()

    def test_installed_version_drift(self):
        (self.dependencies / "node_modules/tool/package.json").write_text('{"version":"2.0.0"}')
        with self.assertRaisesRegex(ValueError, "version differs"):
            self.observe()

    def test_rehashed_inventory_drift(self):
        receipt = self.observe()
        (self.dependencies / "node_modules/tool/cli.js").write_text("different owned bytes")
        with self.assertRaisesRegex(ValueError, "drift"):
            external.validate(receipt, self.source, self.dependencies)

    def test_manifest_terminal_promotion(self):
        receipt = self.observe()
        receipt["actual_component_started"] = True
        payload = {k: v for k, v in receipt.items() if k != "manifest_sha256"}
        receipt["manifest_sha256"] = external.hashlib.sha256(external.canonical(payload)).hexdigest()
        with self.assertRaisesRegex(ValueError, "drift"):
            external.validate(receipt, self.source, self.dependencies)

    def test_source_nested_dependency_rejected(self):
        with self.assertRaisesRegex(ValueError, "outside source"):
            external.observe(self.source, self.source / "web")

    def test_symlink_root(self):
        link = self.root / "link"
        link.symlink_to(self.dependencies, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "symlink ancestor"):
            external.observe(self.source, link)


if __name__ == "__main__":
    unittest.main()
