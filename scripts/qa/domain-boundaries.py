"""Check source ownership, independent of databases and deployment tools."""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[2]
errors = []
for area in ("services", "packages"):
    for path in (ROOT / area).rglob("*.go"):
        if path.name.endswith("_test.go") or path.name.endswith(".pb.go"):
            continue
        rel = path.relative_to(ROOT)
        text = path.read_text(encoding="utf-8")
        owner = rel.parts[1]
        for foreign in re.findall(r'"github.com/quenching/([^/"]+)/internal/[^"]+"', text):
            if foreign != owner:
                errors.append(f"{rel}: imports foreign internal: {foreign}")
        # SQL only: comments/model DTO names are not table access.
        sql = "\n".join(re.findall(r"`([^`]+)`", text, re.S))
        sql += "\n" + "\n".join(re.findall(r'"([^"\n]*(?:SELECT|UPDATE|INSERT|DELETE)[^"\n]*)"', text))
        prohibited = []
        if owner == "fault-engine":
            prohibited = ["defects", "content_defects", "fault_combinations", "content_fault_combinations"]
        elif owner == "training-service":
            prohibited = ["ai_pitfall_configs", "content_ai_pitfall_configs", "session_operations", "sandbox_session_operations", "operation_events", "operation_intents", "operation_texts", "operation_audit_sessions", "operation_remote_seals"]
        if owner == "training-service" and path.name == "session_repo.go" and re.search(r"\bINSERT\s+INTO\s+training_operation_projection\b",sql,re.I):
            errors.append(f"{rel}: user reports must call sandbox source owner")
        for table in prohibited:
            if re.search(r"\b(?:FROM|JOIN|UPDATE|INTO)\s+" + re.escape(table) + r"\b", sql, re.I):
                errors.append(f"{rel}: direct SQL against non-owned table {table}")
        if owner == "go-common" and ("operationevidence" in rel.parts or "reviewcontract" in rel.parts):
            errors.append(f"{rel}: domain implementation or contract still in go-common")
        if owner == "proto" and ("pgx" in text or "database/sql" in text or "*gin." in text):
            errors.append(f"{rel}: runtime/storage dependency inside contract module")
if "--contracts" in sys.argv:
    errors = [e for e in errors if e.startswith("packages") or "imports foreign internal" in e]
if "--configuration" in sys.argv:
    errors = [e for e in errors if "fault-engine" in e or "table ai_pitfall_configs" in e or "table content_ai_pitfall_configs" in e or e.startswith("packages") or "imports foreign internal" in e]
if errors:
    for message in errors:
        print(message)
    print(f"FAIL: {len(errors)} domain boundary violations")
    sys.exit(1)
print("PASS: domain source ownership, contract module and cross-service internal boundaries")
