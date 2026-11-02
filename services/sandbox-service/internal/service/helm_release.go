package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// HelmRelease carries persisted inventory, independent of currently visible resources.
type HelmRelease struct {
	Status   string
	Manifest string
}
type HelmReleaseInspector interface {
	InspectRelease(context.Context, string, string) (HelmRelease, error)
}

func (h *HelmCLI) InspectRelease(ctx context.Context, namespace, release string) (HelmRelease, error) {
	raw, err := exec.CommandContext(ctx, h.BinPath, "status", release, "--namespace", namespace, "--output", "json").CombinedOutput()
	if err != nil {
		return HelmRelease{}, fmt.Errorf("release status unavailable: %w", err)
	}
	var reply struct {
		Info struct {
			Status string `json:"status"`
		} `json:"info"`
		Manifest string `json:"manifest"`
	}
	if len(raw) > 8*1024*1024 || json.Unmarshal(raw, &reply) != nil || reply.Info.Status == "" || reply.Manifest == "" {
		return HelmRelease{}, fmt.Errorf("release inventory unavailable")
	}
	return HelmRelease{Status: reply.Info.Status, Manifest: reply.Manifest}, nil
}
