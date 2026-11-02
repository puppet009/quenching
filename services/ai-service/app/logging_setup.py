"""ai-service 统一日志配置。

特性：
- 同时输出到 stdout 与文件（LOG_DIR 非空时）。
- 文件按服务/本地日期分目录，单文件 100MB，保留 7 个大小轮转副本、30 天。
- HTTP(uvicorn) 与 gRPC 两个进程分别写入独立文件，避免多进程并发写同一文件。

用法（在进程入口尽早调用一次）：
    from app.logging_setup import setup_logging
    setup_logging("ai-service-http")   # 或 "ai-service-grpc"
"""

from __future__ import annotations

import logging
import os
import sys
from datetime import date, datetime, timedelta
from logging.handlers import RotatingFileHandler
from pathlib import Path

# 文本格式：时间 ISO8601 + 毫秒、级别、logger 名、消息。
_LOG_FORMAT = "%(asctime)s %(levelname)s %(name)s %(message)s"

_configured = False


class OffsetFormatter(logging.Formatter):
    def formatTime(self, record, datefmt=None):
        return (
            datetime.fromtimestamp(record.created)
            .astimezone()
            .isoformat(timespec="milliseconds")
        )


class DailyFileHandler(logging.Handler):
    """Each HTTP/gRPC process and Pod owns its files; switch on the first record of a new day."""

    def __init__(self, root: str, file_name: str, retention_days: int):
        super().__init__()
        self.directory = Path(root) / "ai-service"
        self.file_name = Path(file_name).name
        self.retention_days = retention_days
        self.day = ""
        self.sink = None
        pod = os.getenv("POD_NAME", "")
        self.instance = (
            "-" + pod if pod and all(c.isalnum() or c in "-_." for c in pod) else ""
        )

    def emit(self, record):
        try:
            day = datetime.fromtimestamp(record.created).astimezone().date()
            if self.day != day.isoformat():
                if self.sink:
                    self.sink.close()
                    self.sink = None
                directory = self.directory / day.isoformat()
                directory.mkdir(parents=True, exist_ok=True)
                name = Path(self.file_name)
                self.sink = RotatingFileHandler(
                    directory / (name.stem + self.instance + name.suffix),
                    maxBytes=100 * 1024 * 1024,
                    backupCount=7,
                    encoding="utf-8",
                )
                self.day = day.isoformat()
                self._prune(day)
            self.sink.setFormatter(self.formatter)
            self.sink.emit(record)
        except Exception:
            self.handleError(record)

    def _prune(self, today: date):
        if self.retention_days <= 0:
            return
        cutoff = today - timedelta(days=self.retention_days)
        prefix = Path(self.file_name).stem
        for directory in self.directory.iterdir():
            if directory.is_symlink() or not directory.is_dir():
                continue
            try:
                day = date.fromisoformat(directory.name)
            except ValueError:
                continue
            if day.isoformat() != directory.name or day >= cutoff:
                continue
            for file in directory.iterdir():
                if (
                    not file.is_symlink()
                    and file.is_file()
                    and file.name.startswith(prefix)
                    and ".log" in file.suffixes
                ):
                    try:
                        file.unlink()
                    except OSError:
                        pass
            try:
                directory.rmdir()  # Only removes an empty directory.
            except OSError:
                pass

    def close(self):
        self.acquire()
        try:
            if self.sink:
                self.sink.close()
                self.sink = None
        finally:
            self.release()
            super().close()


def setup_logging(file_name: str) -> None:
    """配置 root logger。重复调用安全（只生效一次）。"""
    global _configured
    if _configured:
        return

    # 延迟导入，避免模块导入阶段触发环境变量解析。
    from app.config import get_settings

    settings = get_settings()
    level = _resolve_level(settings.log_level)

    formatter = OffsetFormatter(_LOG_FORMAT)

    root = logging.getLogger()
    root.setLevel(level)
    # 清理 uvicorn / basicConfig 可能预置的 handler，保证格式与去向统一。
    for h in list(root.handlers):
        root.removeHandler(h)
        h.close()

    stream_handler = logging.StreamHandler(sys.stdout)
    stream_handler.setFormatter(formatter)
    root.addHandler(stream_handler)

    if settings.log_dir:
        try:
            os.makedirs(settings.log_dir, exist_ok=True)
            file_handler = DailyFileHandler(
                settings.log_dir, file_name, settings.log_retention_days
            )
            file_handler.setFormatter(formatter)
            root.addHandler(file_handler)
        except OSError as exc:  # 目录不可写时不应阻断启动，退化为仅 stdout。
            root.error("创建日志文件失败，仅输出到 stdout: %s", exc)

    # uvicorn 默认会自带 handler 并把 propagate 关掉，这里改为统一走 root。
    for name in ("uvicorn", "uvicorn.error", "uvicorn.access"):
        lg = logging.getLogger(name)
        lg.handlers = []
        lg.propagate = True
        lg.setLevel(level)

    _configured = True


def _resolve_level(level: str) -> int:
    return getattr(logging, str(level).upper(), logging.INFO)
