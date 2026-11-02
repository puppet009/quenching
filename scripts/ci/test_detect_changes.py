"""Exercise CI selection against real, isolated Git histories (no network)."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("detect_changes.py")


class ChangeSelectionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.git("init", "--quiet")
        self.git("config", "user.name", "CI fixture")
        self.git("config", "user.email", "ci@example.invalid")

    def git(self, *args):
        return subprocess.check_output(
            ["git", "-C", str(self.repo), *args], text=True
        ).strip()

    def commit(self, path, content="fixture"):
        target = self.repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
        self.git("add", "--", path)
        self.git("commit", "--quiet", "-m", "fixture")
        return self.git("rev-parse", "HEAD")

    def detect(self, kind, event_name, event):
        event_path = self.repo / "event.json"
        output_path = self.repo / "outputs.txt"
        event_path.write_text(json.dumps(event), encoding="utf-8")
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--kind", kind],
            cwd=self.repo,
            env={
                **os.environ,
                "GITHUB_EVENT_NAME": event_name,
                "GITHUB_EVENT_PATH": str(event_path),
                "GITHUB_OUTPUT": str(output_path),
            },
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = output_path.read_text(encoding="utf-8").splitlines()
        self.assertTrue(all("=" in line for line in lines), lines)
        return dict(line.split("=", 1) for line in lines)

    def test_first_root_push_checks_all_modules_with_single_line_json(self):
        head = self.commit("README.md")
        output = self.detect("go", "push", {"before": "0" * 40, "after": head})
        self.assertEqual(output["should_check"], "true")
        self.assertEqual(output["check_all"], "true")
        self.assertEqual(
            json.loads(output["services"]),
            ["user-service", "content-service", "sandbox-service", "training-service",
             "fault-engine", "review-service", "api-gateway"],
        )

    def test_multi_commit_push_includes_earlier_changed_app(self):
        before = self.commit("README.md")
        self.commit("apps/web/src/first.ts")
        after = self.commit("apps/admin/src/second.ts")
        output = self.detect("frontend", "push", {"before": before, "after": after})
        self.assertEqual(json.loads(output["apps"]), ["web", "admin"])

    def test_pull_request_uses_merge_base_excluding_base_only_changes(self):
        common = self.commit("README.md")
        head = self.commit("apps/web/src/feature.ts")
        self.git("checkout", "--quiet", "--detach", common)
        base = self.commit("apps/admin/src/base-only.ts")
        output = self.detect("frontend", "pull_request", {
            "pull_request": {"base": {"sha": base}, "head": {"sha": head}}
        })
        self.assertEqual(json.loads(output["apps"]), ["web"])

    def test_manual_run_checks_all_apps_without_needing_parent(self):
        self.commit("README.md")
        output = self.detect("frontend", "workflow_dispatch", {})
        self.assertEqual(json.loads(output["apps"]), ["web", "admin"])
        self.assertEqual(output["check_all"], "true")

    def test_missing_force_push_base_checks_all_instead_of_skipping(self):
        after = self.commit("README.md")
        output = self.detect("frontend", "push", {"before": "1" * 40, "after": after})
        self.assertEqual(json.loads(output["apps"]), ["web", "admin"])

    def test_deleted_service_file_still_selects_service(self):
        before = self.commit("services/user-service/removed.go")
        self.git("rm", "--quiet", "services/user-service/removed.go")
        self.git("commit", "--quiet", "-m", "delete")
        after = self.git("rev-parse", "HEAD")
        output = self.detect("go", "push", {"before": before, "after": after})
        self.assertEqual(json.loads(output["services"]), ["user-service"])

    def test_spaces_unicode_and_shell_characters_remain_filename_data(self):
        before = self.commit("README.md")
        after = self.commit("apps/web/src/中文 $(touch injected).ts")
        output = self.detect("frontend", "push", {"before": before, "after": after})
        self.assertEqual(json.loads(output["apps"]), ["web"])
        self.assertFalse((self.repo / "injected").exists())

    def test_workflow_lock_config_and_shared_changes_check_all_apps(self):
        for path in (
            ".github/workflows/frontend-ci.yml", "pnpm-lock.yaml", "package.json",
            "pnpm-workspace.yaml", ".npmrc", "eslint.config.js",
            "packages/shared/src/index.ts", "scripts/ci/detect_changes.py",
        ):
            with self.subTest(path=path):
                before = self.commit("README.md", path)
                after = self.commit(path)
                output = self.detect("frontend", "push", {"before": before, "after": after})
                self.assertEqual(json.loads(output["apps"]), ["web", "admin"])
                self.assertEqual(output["check_all"], "true")

    def test_go_workspace_lint_shared_and_script_changes_check_all_services(self):
        for path in ("go.work", "go.work.sum", ".golangci.yml",
                     "packages/proto/go.sum", "scripts/check_structure.py"):
            with self.subTest(path=path):
                before = self.commit("README.md", path)
                after = self.commit(path)
                output = self.detect("go", "push", {"before": before, "after": after})
                self.assertEqual(len(json.loads(output["services"])), 7)
                self.assertEqual(output["check_all"], "true")

    def test_unrelated_docs_change_skips_expensive_checks(self):
        before = self.commit("README.md")
        after = self.commit("docs/notes.md")
        output = self.detect("frontend", "push", {"before": before, "after": after})
        self.assertEqual(output["apps"], "[]")
        self.assertEqual(output["should_check"], "false")


if __name__ == "__main__":
    unittest.main()
