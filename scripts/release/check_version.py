#!/usr/bin/env python3
"""Check the platform release version against deployment and package metadata."""

import ast
import json
from pathlib import Path
import re
import sys
import tomllib


ROOT = Path(__file__).resolve().parents[2]


def main():
    version = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))["version"]
    match = re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(alpha|beta|rc)\.(0|[1-9]\d*))?", version)
    if not match:
        print("Invalid platform version: use X.Y.Z or X.Y.Z-{alpha,beta,rc}.N", file=sys.stderr)
        return 1
    python_version = ".".join(match.group(i) for i in (1, 2, 3))
    if match.group(4):
        python_version += {"alpha": "a", "beta": "b", "rc": "rc"}[match.group(4)] + match.group(5)
    errors = []

    def expect(path, actual, expected=version):
        if actual != expected:
            errors.append(f"{path}: expected {expected!r}, got {actual!r}")

    for path in ("apps/web/package.json", "apps/admin/package.json", "packages/shared/package.json"):
        expect(path, json.loads((ROOT / path).read_text(encoding="utf-8")).get("version"))
    metadata = tomllib.loads((ROOT / "services/ai-service/pyproject.toml").read_text(encoding="utf-8"))
    expect("services/ai-service/pyproject.toml", metadata["project"]["version"], python_version)
    app = ast.parse((ROOT / "services/ai-service/app/main.py").read_text(encoding="utf-8"))
    api_versions = [keyword.value.value for node in ast.walk(app)
                    if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == "FastAPI"
                    for keyword in node.keywords if keyword.arg == "version" and isinstance(keyword.value, ast.Constant)]
    expect("services/ai-service/app/main.py", api_versions, [version])
    for path, fields in (("deploy/helm/Chart.yaml", ("version", "appVersion")),
                         ("deploy/helm/values.yaml", ("imageTag",)),
                         ("deploy/server/values-server.yaml", ("imageTag",))):
        text = (ROOT / path).read_text(encoding="utf-8")
        for field in fields:
            values = re.findall(rf'^\s*{field}:\s*["\']?([\w.-]+)', text, re.M)
            expect(f"{path} ({field})", values, [version])
    path = "deploy/server/manifests/20-frontend.yaml"
    frontend = re.findall(r'^\s*image:\s*quenching/(web|admin):([\w.-]+)',
                          (ROOT / path).read_text(encoding="utf-8"), re.M)
    expect(path, sorted(frontend), [("admin", version), ("web", version)])
    for filename in ("images.sh", "migrate.sh", "deploy.sh", "upgrade-observability.sh"):
        path = "deploy/server/" + filename
        defaults = re.findall(r'\$\{IMAGE_TAG:-([^}]+)\}', (ROOT / path).read_text(encoding="utf-8"))
        expect(path, defaults, [version])
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"PASS: platform {version}, Python metadata {python_version}; template and migration versions remain independent")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
