import asyncio
import unittest
from app.services.brain import (
    BrainRequest,
    BrainDecision,
    build_brain_messages,
    decide_brain,
)
from app.adapters.llm_client import LLMResult


class BrainTests(unittest.TestCase):
    def request(self, **kw):
        values = dict(
            session_id=1,
            phase="calibration",
            observation_version=2,
            context_json='{"public":"latency"}',
            evidence_refs=["e1"],
            allowed_tools=[
                {
                    "name": "wait_for_event",
                    "description": "wait",
                    "parameters_json": '{"type":"object","additionalProperties":false}',
                }
            ],
        )
        values.update(kw)
        return BrainRequest(**values)

    def test_brain_roundtrip(self):
        class RecordedInference:
            async def complete(self, messages, **kwargs):
                return LLMResult(
                    content='{"decision_id":"d1","observation_version":2,"learning_objective":"collect evidence","action":"wait_for_event","parameters_json":"{}","evidence_refs":["e1"],"reason_summary":"need regression","next_observation":"business_result"}',
                    model="qwen3.8-flash",
                    actual_model="qwen3.8-flash",
                    request_id="r1",
                )

        reply = asyncio.run(decide_brain(self.request(), client=RecordedInference()))
        decision = BrainDecision.model_validate_json(reply.decision_json)
        self.assertEqual(decision.action, "wait_for_event")
        self.assertEqual(reply.actual_model, "qwen3.8-flash")
        self.assertEqual(reply.request_id, "r1")

    def test_unknown_action(self):
        with self.assertRaises(ValueError):
            self.request(
                allowed_tools=[
                    {
                        "name": "exec_shell",
                        "description": "unsafe",
                        "parameters_json": "{}",
                    }
                ]
            )

    def test_tool_result_id_mismatch(self):
        req = self.request(
            tool_results=[{"call_id": "wrong", "content_json": "{}", "is_error": False}]
        )
        with self.assertRaises(ValueError):
            build_brain_messages(req)

    def test_director_context_not_in_chat(self):
        req = self.request(context_json='{"ground_truth":"director-only"}')
        messages = build_brain_messages(req)
        self.assertIn("director-only", messages[1]["content"])
        self.assertEqual(messages[0]["role"], "system")
        self.assertNotIn("conversation_id", req.model_dump())
        self.assertNotIn("history_json", req.model_dump())

    def test_invalid_evidence_and_stale_version_rejected(self):
        class BadInference:
            async def complete(self, *a, **kw):
                return LLMResult(
                    content='{"decision_id":"d1","observation_version":1,"learning_objective":"x","action":"wait_for_event","parameters_json":"{}","evidence_refs":["foreign"],"reason_summary":"x","next_observation":"x"}',
                    model="qwen3.8-flash",
                )

        with self.assertRaises(ValueError):
            asyncio.run(decide_brain(self.request(), client=BadInference()))

    def test_correction_counts_actual_usage(self):
        class Recorded:
            calls = 0

            async def complete(self, *args, **kwargs):
                self.calls += 1
                good = '{"decision_id":"d1","observation_version":2,"learning_objective":"collect evidence","action":"wait_for_event","parameters_json":"{}","evidence_refs":["e1"],"reason_summary":"need regression","next_observation":"business_result"}'
                return LLMResult(
                    content="{}" if self.calls == 1 else good,
                    model="qwen3.8-flash",
                    actual_model="qwen3.8-flash",
                    prompt_tokens=3,
                    completion_tokens=2,
                    latency_ms=1,
                )

        reply = asyncio.run(decide_brain(self.request(), client=Recorded()))
        self.assertEqual(reply.prompt_tokens, 6)
        self.assertEqual(reply.completion_tokens, 4)
        self.assertEqual(reply.latency_ms, 2)
