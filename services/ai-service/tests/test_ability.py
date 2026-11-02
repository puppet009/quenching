import unittest
from app.services.ability import AbilityAssessment, validate_assessment


class AbilityTests(unittest.TestCase):
    def assessment(self):
        return {
            "session_id": 1,
            "version": 1,
            "dimensions": {
                key: {
                    "level": "intermediate",
                    "confidence": 0.9,
                    "evidence_refs": ["e1"],
                }
                for key in (
                    "localization",
                    "evidence",
                    "mechanism",
                    "repair",
                    "regression",
                    "architecture",
                    "hint_dependence",
                )
            },
            "evidence_refs": ["e1"],
            "sufficiency": "sufficient",
            "overall_level": "intermediate",
        }

    def test_assessment_requires_evidence(self):
        value = self.assessment()
        value["dimensions"]["repair"]["evidence_refs"] = []
        with self.assertRaises(ValueError):
            AbilityAssessment.model_validate(value)

    def test_foreign_evidence_refused(self):
        with self.assertRaises(ValueError):
            validate_assessment(
                AbilityAssessment.model_validate(self.assessment()),
                ["other"],
                {"challenges": []},
            )

    def test_single_fast_fix_does_not_promote(self):
        result = validate_assessment(
            AbilityAssessment.model_validate(self.assessment()),
            ["e1"],
            {"challenges": [{"status": "verified_repair", "defect_ids": [1]}]},
        )
        self.assertEqual(result.overall_level, "unknown")

    def test_cleanup_not_repair(self):
        result = validate_assessment(
            AbilityAssessment.model_validate(self.assessment()),
            ["e1"],
            {
                "challenges": [
                    {"status": "closed_unresolved", "defect_ids": [1]},
                    {"status": "closed_unresolved", "defect_ids": [2]},
                ]
            },
        )
        self.assertEqual(result.overall_level, "unknown")
