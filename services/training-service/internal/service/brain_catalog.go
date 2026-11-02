package service

import (
	"encoding/json"
	"errors"
	"github.com/quenching/training-service/internal/model"
)

func BrainCatalogAvailability(raw string) (model.BrainPhase, string, error) {
	var data struct {
		Faults []BrainTrainableFault `json:"faults"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return "", "", err
	}
	if fields["faults"] == nil {
		return "", "", errors.New("missing faults")
	}
	if len(data.Faults) == 0 {
		return model.BrainDegraded, "no_verified_faults", nil
	}
	return model.BrainCalibration, "", nil
}
