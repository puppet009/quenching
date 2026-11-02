import unittest
from app.services.review import validate_brain_review_sections


class BrainReviewTests(unittest.TestCase):
    def test_industry_advice_is_not_observed_fact(self):
        brain = {"evidence": [{"id": "e1"}], "sources": []}
        validate_brain_review_sections(
            {
                "claims": [
                    {
                        "category": "industry_advice",
                        "text": "Consider a cache boundary",
                        "evidence_refs": [],
                    }
                ]
            },
            brain,
        )
        with self.assertRaises(ValueError):
            validate_brain_review_sections(
                {
                    "claims": [
                        {
                            "category": "observed",
                            "text": "User repaired",
                            "evidence_refs": [],
                        }
                    ]
                },
                brain,
            )

    def test_source_must_be_supplied(self):
        with self.assertRaises(ValueError):
            validate_brain_review_sections(
                {
                    "claims": [
                        {
                            "category": "industry_advice",
                            "text": "incident comparison",
                            "source_urls": ["https://invented.example/incident"],
                        }
                    ]
                },
                {"evidence": [], "sources": []},
            )

    def test_uncited_chapter_text_cannot_bypass_claims(self):
        sections = {
            "root_cause": "fabricated repair",
            "claims": [{"category": "industry_advice", "text": "consider isolation"}],
        }
        validate_brain_review_sections(sections, {"evidence": [], "sources": []})
        self.assertNotIn("fabricated repair", sections["root_cause"])
