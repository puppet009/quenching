package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type TemplatePaymentAdapter struct {
	kc        *K8sClient
	registry  templates.Registry
	sandboxes BehaviorSandboxReader
	http      *http.Client
}

func NewTemplatePaymentAdapter(kc *K8sClient, registry templates.Registry, sandboxes BehaviorSandboxReader) *TemplatePaymentAdapter {
	return &TemplatePaymentAdapter{kc, registry, sandboxes, &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (a *TemplatePaymentAdapter) resolve(ctx context.Context, sid int64) (string, string, *templates.SandboxPaymentAdapter, error) {
	sb, err := a.sandboxes.GetBySessionID(ctx, sid)
	if err != nil {
		return "", "", nil, err
	}
	if !SandboxPaymentActive(sb) || sb.Namespace != model.NamespaceName(sid) || !a.kc.RealCluster() {
		return "", "", nil, errors.New("payment sandbox unavailable")
	}
	var candidates []templates.Manifest
	for _, manifest := range a.registry.List() {
		if manifest.BehaviorProtocol == nil || manifest.BehaviorProtocol.PaymentAdapter == nil {
			continue
		}
		pods, err := a.kc.clientset.CoreV1().Pods(sb.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + manifest.Slug})
		if err != nil {
			return "", "", nil, err
		}
		if len(pods.Items) > 0 {
			candidates = append(candidates, manifest)
		}
	}
	if len(candidates) != 1 {
		return "", "", nil, errors.New("unique deployed payment adapter required")
	}
	m := candidates[0]
	adapter := m.BehaviorProtocol.PaymentAdapter
	secret, err := a.kc.clientset.CoreV1().Secrets(sb.Namespace).Get(ctx, m.Slug+"-credentials", metav1.GetOptions{})
	if err != nil {
		return "", "", nil, err
	}
	key := string(secret.Data[adapter.SecretKey])
	if len(key) < 32 {
		return "", "", nil, errors.New("payment credentials unavailable")
	}
	for _, s := range m.Services {
		if s.Name == m.BehaviorProtocol.APIService {
			return fmt.Sprintf("http://%s.%s.svc:%d", s.Name, sb.Namespace, s.Port), key, adapter, nil
		}
	}
	return "", "", nil, errors.New("payment target unavailable")
}
func (a *TemplatePaymentAdapter) call(ctx context.Context, method, base, path, key string, body []byte) (SandboxPaymentOrder, error) {
	timestamp := time.Now().Unix()
	signed := body
	if method == "GET" {
		signed = []byte("GET\n" + path)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return SandboxPaymentOrder{}, err
	}
	req.Header.Set("X-Sandbox-Payment-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Sandbox-Payment-Signature", SignSandboxPayment(key, timestamp, signed))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return SandboxPaymentOrder{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return SandboxPaymentOrder{}, errors.New("sandbox payment adapter rejected request")
	}
	var result struct {
		Code int                 `json:"code"`
		Data SandboxPaymentOrder `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1048576)).Decode(&result); err != nil {
		return SandboxPaymentOrder{}, err
	}
	if result.Code != 200 {
		return SandboxPaymentOrder{}, errors.New("sandbox payment operation failed")
	}
	return result.Data, nil
}
func (a *TemplatePaymentAdapter) Inspect(ctx context.Context, sid int64, id string) (SandboxPaymentOrder, error) {
	numeric, err := strconv.ParseInt(id, 10, 64)
	if err != nil || numeric <= 0 || strconv.FormatInt(numeric, 10) != id {
		return SandboxPaymentOrder{}, errors.New("invalid order identity")
	}
	base, key, adapter, err := a.resolve(ctx, sid)
	if err != nil {
		return SandboxPaymentOrder{}, err
	}
	path := strings.ReplaceAll(adapter.InspectPath, "{order_id}", id)
	return a.call(ctx, "GET", base, path, key, nil)
}
func (a *TemplatePaymentAdapter) Callback(ctx context.Context, sid int64, id string, order model.SandboxOrder) (SandboxPaymentOrder, error) {
	base, key, adapter, err := a.resolve(ctx, sid)
	if err != nil {
		return SandboxPaymentOrder{}, err
	}
	raw, err := json.Marshal(struct {
		CommandID string `json:"command_id"`
		model.SandboxOrder
	}{id, order})
	if err != nil {
		return SandboxPaymentOrder{}, err
	}
	return a.call(ctx, "POST", base, adapter.CallbackPath, key, raw)
}
