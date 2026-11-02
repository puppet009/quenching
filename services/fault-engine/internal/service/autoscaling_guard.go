package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/quenching/fault-engine/internal/model"
	"io"
	"net/http"
	"strings"
	"time"
)

type AutoscalingGuard interface {
	Check(context.Context, string) error
	Acquire(context.Context, string, string, string) error
	Release(context.Context, string, string) error
}
type HTTPAutoscalingGuard struct {
	BaseURL, Token string
	Client         *http.Client
}

func (g HTTPAutoscalingGuard) call(ctx context.Context, action, ns, id, reason string) error {
	if g.BaseURL == "" || g.Token == "" {
		return errors.New("resource fault autoscaling guard not configured")
	}
	raw, _ := json.Marshal(map[string]string{"namespace": ns, "hold_id": id, "reason": reason})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.BaseURL, "/")+"/api/internal/v1/resources/scaling-holds/"+action, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", g.Token)
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, e := client.Do(req)
	if e != nil {
		return fmt.Errorf("autoscaling guard: %w", e)
	}
	defer resp.Body.Close()
	var result struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result); e != nil {
		return e
	}
	if resp.StatusCode != 200 || result.Code == nil || *result.Code != 0 {
		return fmt.Errorf("autoscaling guard rejected: %s", result.Msg)
	}
	return nil
}
func (g HTTPAutoscalingGuard) Acquire(ctx context.Context, ns, id, reason string) error {
	return g.call(ctx, "acquire", ns, id, reason)
}
func (g HTTPAutoscalingGuard) Check(ctx context.Context, ns string) error {
	return g.call(ctx, "check", ns, "", "")
}
func (g HTTPAutoscalingGuard) Release(ctx context.Context, ns, id string) error {
	return g.call(ctx, "release", ns, id, "")
}
func resourceFault(spec model.InjectSpec) bool {
	return spec.FreezeAutoscaling || spec.Kind == model.KindCPU || spec.Kind == model.KindMem || spec.Kind == model.KindDisk || spec.Kind == model.KindThreadpool
}

// Call before storing an experiment; this identity must survive an unknown execution result.
func PrepareScalingSpec(spec model.InjectSpec, resourceClass bool) model.InjectSpec {
	spec.FreezeAutoscaling = spec.FreezeAutoscaling || resourceClass
	if resourceFault(spec) && spec.ScalingHoldID == "" {
		spec.ScalingHoldID = uuid.NewString()
	}
	return spec
}
func (in *Injector) SetAutoscalingGuard(g AutoscalingGuard)      { in.guard = g }
func (s *RuntimeService) SetAutoscalingGuard(g AutoscalingGuard) { s.inj.SetAutoscalingGuard(g) }
