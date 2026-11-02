package service

import (
	"github.com/quenching/fault-engine/internal/model"
	"testing"
)

func TestCombinationNotAccepted(t *testing.T) {
	if ValidateFaultCombination(model.FaultCombination{DefectIDs: []int64{1, 2}, TemplateSlug: "mall"}, "digest") == nil {
		t.Fatal("unaccepted combination used")
	}
}
func TestCombinationBindingChanged(t *testing.T) {
	c := model.FaultCombination{DefectIDs: []int64{1, 2}, TemplateSlug: "mall", Status: "verified", AcceptanceDigest: "old", EvidenceJSON: `{"baseline":"e1","combined":"e2","recovery":"e3","isolation":"e4","source":"e5"}`}
	if ValidateFaultCombination(c, "new") == nil {
		t.Fatal("changed binding kept combo acceptance")
	}
}
