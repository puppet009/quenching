package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"slices"
)

type BehaviorSandboxReader interface {
	GetBySessionID(context.Context, int64) (*model.Sandbox, error)
}
type BehaviorCredentialWriter interface {
	SetRunnerTokenHash(context.Context, int64, string) error
}
type TemplateBehaviorPreparer struct {
	runtime     *BehaviorRuntime
	registry    templates.Registry
	sandboxes   BehaviorSandboxReader
	credentials BehaviorCredentialWriter
	commandBase string
}

func NewTemplateBehaviorPreparer(r *BehaviorRuntime, registry templates.Registry, sandboxes BehaviorSandboxReader, credentials BehaviorCredentialWriter, base string) *TemplateBehaviorPreparer {
	return &TemplateBehaviorPreparer{r, registry, sandboxes, credentials, base}
}
func (p *TemplateBehaviorPreparer) PrepareBehavior(ctx context.Context, cmd model.BehaviorCommand) error {
	sb, err := p.sandboxes.GetBySessionID(ctx, cmd.SessionID)
	if err != nil {
		return err
	}
	if sb == nil || (sb.Status != model.StatusReady && sb.Status != model.StatusRunning) || !sb.CompletedAt.IsZero() || sb.Namespace != model.NamespaceName(cmd.SessionID) {
		return errors.New("sandbox not available")
	}
	manifest, ok := p.registry.Get(cmd.TemplateSlug)
	if !ok || manifest.BehaviorProtocol == nil || !slices.Contains(manifest.BehaviorProtocol.Capabilities, cmd.Operation) {
		return errors.New("capability_unavailable")
	}
	// Confirm slug against the actually deployed release in this sandbox, never caller choice.
	if !p.runtime.kc.RealCluster() {
		return errors.New("behavior runtime degraded: real Kubernetes required")
	}
	pods, err := p.runtime.kc.clientset.CoreV1().Pods(sb.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + manifest.Slug})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return errors.New("requested template is not deployed in sandbox")
	}
	secretName := "behavior-runner-credentials"
	secrets := p.runtime.kc.clientset.CoreV1().Secrets(sb.Namespace)
	secret, err := secrets.Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		var nonce [32]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName}, Data: map[string][]byte{"token": []byte(hex.EncodeToString(nonce[:]))}}
		secret, err = secrets.Create(ctx, desired, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, secretName, metav1.GetOptions{})
		}
	}
	if err != nil {
		return err
	}
	token := secret.Data["token"]
	if len(token) < 32 {
		return errors.New("runner secret unavailable")
	}
	hash := sha256.Sum256(token)
	if err = p.credentials.SetRunnerTokenHash(ctx, cmd.SessionID, hex.EncodeToString(hash[:])); err != nil {
		return err
	}
	raw, err := json.Marshal(manifest.BehaviorProtocol)
	if err != nil {
		return err
	}
	var protocol map[string]any
	if err = json.Unmarshal(raw, &protocol); err != nil {
		return err
	}
	endpoint := func(name string) (string, error) {
		for _, s := range manifest.Services {
			if s.Name == name {
				return fmt.Sprintf("http://%s.%s.svc:%d", s.Name, sb.Namespace, s.Port), nil
			}
		}
		return "", errors.New("protocol service unavailable")
	}
	entry, err := endpoint(manifest.BehaviorProtocol.EntryService)
	if err != nil {
		return err
	}
	api, err := endpoint(manifest.BehaviorProtocol.APIService)
	if err != nil {
		return err
	}
	protocol["entry_url"] = entry
	protocol["api_url"] = api
	raw, err = json.Marshal(protocol)
	if err != nil {
		return err
	}
	st, err := p.runtime.Start(ctx, BehaviorStartConfig{SessionID: cmd.SessionID, EntryURL: entry, Flow: &BehaviorFlowConfig{Name: "command_protocol", EntryURL: entry, Protocol: raw}, CommandURL: fmt.Sprintf("%s/internal/v1/behavior-runner/%d", p.commandBase, cmd.SessionID), RunnerSecret: secretName})
	if err != nil {
		return err
	}
	if st.Phase == BehaviorPhaseDegraded || st.Phase == BehaviorPhaseFailed {
		return errors.New("behavior runtime unavailable")
	}
	return nil
}
