package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/quenching/fault-engine/internal/model"
	"slices"
)

func ValidateFaultCombination(c model.FaultCombination, currentDigest string) error {
	if c.TemplateSlug == "" || len(c.DefectIDs) != 2 || c.DefectIDs[0] <= 0 || c.DefectIDs[1] <= 0 || c.DefectIDs[0] == c.DefectIDs[1] || c.Status != "verified" || c.AcceptanceDigest == "" || c.AcceptanceDigest != currentDigest {
		return errors.New("combination not accepted for current artifacts")
	}
	var evidence map[string]string
	if json.Unmarshal([]byte(c.EvidenceJSON), &evidence) != nil {
		return errors.New("invalid combination evidence")
	}
	for _, key := range []string{"baseline", "combined", "recovery", "isolation", "source"} {
		if evidence[key] == "" {
			return errors.New("incomplete combination acceptance")
		}
	}
	return nil
}
func FaultAcceptanceDigest(templateDigest string, defects []*model.Defect) string {
	ordered := slices.Clone(defects)
	slices.SortFunc(ordered, func(a, b *model.Defect) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	raw, _ := json.Marshal(struct {
		Template string
		Defects  []*model.Defect
	}{templateDigest, ordered})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
