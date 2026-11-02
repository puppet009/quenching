"""Explicit paid server probe of the production Qwen adapter; never prints credentials/content."""
import argparse
import asyncio
import json
import os
from pathlib import Path
import sys

async def run():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--allow-paid", action="store_true", help="authorize four real inference requests")
    args = parser.parse_args()
    if not args.allow_paid:
        parser.error("real provider requests require --allow-paid")
    if not os.environ.get("LLM_API_KEY") or os.environ.get("LLM_MOCK", "true").lower() != "false":
        raise SystemExit("FAIL: set LLM_API_KEY and LLM_MOCK=false in the server environment")
    if os.environ.get("LLM_MODEL") != "qwen3.8-flash" or os.environ.get("APP_ENV") != "production":
        raise SystemExit("FAIL: APP_ENV=production and LLM_MODEL=qwen3.8-flash required")
    repo = Path(__file__).resolve().parents[2]
    local = repo / "services" / "ai-service"
    if local.is_dir():
        sys.path.insert(0, str(local))
    from app.adapters.llm_client import llm_client
    from app.adapters.qwen_policy import policy_for
    try:
        for task in ("director", "assistant", "assessment", "review"):
            result = await llm_client.complete(
                [{"role":"system","content":"Return exactly a JSON object with ok=true."},
                 {"role":"user","content":"Connectivity probe; do not use any tools."}],
                response_schema={"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]},
                task=task,
            )
            if result.actual_model != "qwen3.8-flash" or json.loads(result.content).get("ok") is not True:
                raise ValueError("response contract failed")
            print(json.dumps({"task":task,"status":"PASS","actual_model":result.actual_model,
                              "thinking":policy_for(task).enable_thinking,"evidence":"live_adapter_probe"}))
    finally:
        await llm_client.aclose()

if __name__ == "__main__":
    try:
        asyncio.run(run())
    except Exception as exc:
        print(json.dumps({"status":"FAIL","error_type":type(exc).__name__}))
        sys.exit(1)
