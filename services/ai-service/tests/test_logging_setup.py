"""Real file output, local-date rollover, and offset-bearing timestamps."""

import logging
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

from app import logging_setup


class LoggingFilesTest(unittest.TestCase):
    def test_setup_captures_application_and_uvicorn_logs_once(self):
        with tempfile.TemporaryDirectory() as root:
            env = {
                **os.environ,
                "LOG_DIR": root,
                "POD_NAME": "ai-test",
                "LOG_LEVEL": "info",
            }
            result = subprocess.run(
                [
                    sys.executable,
                    "-c",
                    """
import logging
from app.logging_setup import setup_logging
setup_logging("ai-service-http.log")
setup_logging("ignored.log")
logging.getLogger("assistant").info("application message")
logging.getLogger("uvicorn.error").info("server message")
logging.getLogger("uvicorn.access").info("access message")
logging.shutdown()
""",
                ],
                env=env,
                capture_output=True,
                text=True,
                check=True,
            )
            files = list(Path(root).glob("ai-service/*/ai-service-http-ai-test.log"))
            self.assertEqual(len(files), 1)
            content = files[0].read_text(encoding="utf-8")
            for message in ["application message", "server message", "access message"]:
                self.assertEqual(content.count(message), 1)
                self.assertEqual(result.stdout.count(message), 1)
            self.assertEqual(len(list(Path(root).rglob("*.log"))), 1)

    def test_records_are_separated_by_service_date_and_process(self):
        with tempfile.TemporaryDirectory() as root:
            handler = logging_setup.DailyFileHandler(root, "ai-service-http.log", 30)
            handler.setFormatter(
                logging_setup.OffsetFormatter("%(asctime)s %(message)s")
            )
            try:
                for day, message in [(16, "first day"), (17, "next day")]:
                    record = logging.LogRecord(
                        "assistant", logging.INFO, __file__, 1, message, (), None
                    )
                    record.created = (
                        datetime(2026, 10, day, 12, tzinfo=timezone.utc).timestamp()
                        + 0.123
                    )
                    handler.handle(record)
                files = sorted(Path(root).glob("ai-service/*/ai-service-http*.log"))
                self.assertEqual(len(files), 2)
                for file, message in zip(files, ["first day", "next day"]):
                    content = file.read_text(encoding="utf-8").strip()
                    stamp, text = content.split(" ", 1)
                    self.assertEqual(text, message)
                    self.assertIsNotNone(datetime.fromisoformat(stamp).tzinfo)
                    self.assertEqual(datetime.fromisoformat(stamp).microsecond, 123000)
            finally:
                handler.close()

    def test_retention_preserves_unrelated_files(self):
        with tempfile.TemporaryDirectory() as root:
            old = Path(root) / "ai-service" / "2026-09-01"
            old.mkdir(parents=True)
            (old / "ai-service-http-old.log").write_text("expired")
            (old / "manual.txt").write_text("keep")
            handler = logging_setup.DailyFileHandler(root, "ai-service-http.log", 30)
            try:
                record = logging.LogRecord(
                    "assistant", logging.INFO, __file__, 1, "new", (), None
                )
                record.created = datetime(
                    2026, 10, 17, 12, tzinfo=timezone.utc
                ).timestamp()
                handler.handle(record)
                self.assertFalse((old / "ai-service-http-old.log").exists())
                self.assertTrue((old / "manual.txt").exists())
            finally:
                handler.close()
