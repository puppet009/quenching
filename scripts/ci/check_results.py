#!/usr/bin/env python3
"""Make CI summaries fail closed, including detection and dependency failures."""

import argparse
import json
import os
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--required", nargs="+", required=True)
    parser.add_argument("--conditional", nargs="*", default=[])
    args = parser.parse_args()
    needs = json.loads(os.environ["CI_NEEDS"])
    lines = ["## CI results", ""]
    passed = True
    for job in args.required:
        status = needs.get(job, {}).get("result", "missing")
        lines.append(f"- {job}: {status}")
        passed &= status == "success"
    if args.conditional:
        detector = needs.get("detect-changes", {})
        selected = detector.get("outputs", {}).get("should_check")
        passed &= detector.get("result") == "success" and selected in ("true", "false")
        for job in args.conditional:
            status = needs.get(job, {}).get("result", "missing")
            lines.append(f"- {job}: {status}")
            passed &= status == "success" or (selected == "false" and status == "skipped")
    report = "\n".join(lines) + "\n"
    print(report)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a", encoding="utf-8") as stream:
            stream.write(report)
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
