#!/usr/bin/env python3
"""Select CI targets from GitHub event data, never from shell interpolation.

Checkouts must use fetch-depth: 0. New repositories, manual runs, and unavailable
history deliberately check everything; ordinary pushes compare before..after,
and PRs compare the head against its merge base with the base SHA.
"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess


SERVICES = [
    "user-service", "content-service", "sandbox-service", "training-service",
    "fault-engine", "review-service", "api-gateway",
]
APPS = ["web", "admin"]


def git(*args):
    return subprocess.check_output(["git", *args], stderr=subprocess.PIPE)


def available_commit(sha):
    # Only object IDs are accepted; event values can never become Git options.
    if not isinstance(sha, str) or not re.fullmatch(r"[0-9a-fA-F]{40,64}", sha):
        return False
    try:
        git("cat-file", "-e", sha + "^{commit}")
        return True
    except subprocess.CalledProcessError:
        return False


def changed_files(event_name, event):
    if event_name == "workflow_dispatch":
        return None
    if event_name == "push":
        before, after = event.get("before"), event.get("after")
    elif event_name == "pull_request":
        pr = event.get("pull_request", {})
        before = pr.get("base", {}).get("sha")
        after = pr.get("head", {}).get("sha")
    else:
        raise ValueError(f"Unsupported event: {event_name}")

    if not available_commit(before) or not available_commit(after):
        print("Full check: new branch/root push or event history is unavailable")
        return None
    if event_name == "pull_request":
        try:
            before = git("merge-base", before, after).decode("ascii").strip()
        except subprocess.CalledProcessError:
            print("Full check: PR histories have no available merge base")
            return None
    # NUL-delimited paths preserve spaces, newlines, Unicode, and shell syntax.
    paths = git("diff", "--name-only", "--no-renames", "-z", before, after, "--")
    return [os.fsdecode(path) for path in paths.split(b"\0") if path]


def select(kind, paths):
    targets = SERVICES if kind == "go" else APPS
    prefix = "services" if kind == "go" else "apps"
    common = {"Makefile"}
    if kind == "go":
        common |= {"go.work", "go.work.sum", ".golangci.yml"}
    else:
        common |= {
            "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".npmrc",
            ".pnpmfile.cjs", ".node-version", ".nvmrc",
        }
    full = paths is None or any(
        path in common
        or path.startswith(("packages/", "scripts/", ".github/workflows/"))
        or (kind == "frontend" and "/" not in path and (
            "eslint" in path or path.startswith("tsconfig")
        ))
        for path in paths
    )
    selected = targets if full else [
        target for target in targets
        if any(path.startswith(f"{prefix}/{target}/") for path in paths)
    ]
    return {
        "services" if kind == "go" else "apps": json.dumps(selected, separators=(",", ":")),
        "should_check": str(bool(selected)).lower(),
        "check_all": str(full).lower(),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kind", choices=("go", "frontend"), required=True)
    args = parser.parse_args()
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
    outputs = select(args.kind, changed_files(os.environ["GITHUB_EVENT_NAME"], event))
    with Path(os.environ["GITHUB_OUTPUT"]).open("a", encoding="utf-8", newline="\n") as stream:
        for name, value in outputs.items():
            print(f"{name}={value}", file=stream)
            print(f"{name}={value}")


if __name__ == "__main__":
    main()
