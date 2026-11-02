"""A required summary must fail when a prerequisite or selected check fails."""

import json
import os
from pathlib import Path
import subprocess
import sys
import unittest


SCRIPT = Path(__file__).with_name("check_results.py")


class SummaryTests(unittest.TestCase):
    def check(self, detector="success", selected="true", checks="success", guards="success"):
        return subprocess.run(
            [sys.executable, str(SCRIPT), "--required", "detect-changes", "guards",
             "--conditional", "checks"],
            env={**os.environ, "CI_NEEDS": json.dumps({
                "detect-changes": {"result": detector, "outputs": {"should_check": selected}},
                "guards": {"result": guards},
                "checks": {"result": checks},
            })},
            text=True, capture_output=True,
        )

    def test_selected_checks_must_succeed(self):
        self.assertEqual(self.check().returncode, 0)
        for status in ("failure", "cancelled", "skipped"):
            with self.subTest(status=status):
                self.assertNotEqual(self.check(checks=status).returncode, 0)

    def test_detector_failure_cannot_be_hidden_by_skipped_jobs(self):
        self.assertNotEqual(self.check(detector="failure", selected="", checks="skipped").returncode, 0)

    def test_unselected_checks_may_skip_only_after_successful_detection(self):
        self.assertEqual(self.check(selected="false", checks="skipped").returncode, 0)
        self.assertNotEqual(self.check(selected="false", checks="failure").returncode, 0)
        self.assertNotEqual(self.check(selected="", checks="skipped").returncode, 0)

    def test_always_required_guard_cannot_skip(self):
        self.assertNotEqual(self.check(guards="skipped", selected="false", checks="skipped").returncode, 0)


if __name__ == "__main__":
    unittest.main()
