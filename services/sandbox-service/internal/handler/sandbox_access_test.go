package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type completedAccessStore struct {
	service.ActivityPort
	sb *model.Sandbox
}

func (s *completedAccessStore) GetBySessionID(context.Context, int64) (*model.Sandbox, error) {
	return s.sb, nil
}
func (s *completedAccessStore) MarkCompleted(_ context.Context, _ int64, at time.Time, _ string) error {
	s.sb.CompletedAt = at
	return nil
}

func TestCompleteRevokesProjectAccessBeforeAcknowledgement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	kc := service.NewK8sClient(cs)
	if _, err := kc.EnsureSandboxResources(ctx, "71", "9"); err != nil {
		t.Fatal(err)
	}
	store := &completedAccessStore{sb: &model.Sandbox{SessionID: 71, UserID: 9, Namespace: "sandbox-71", Status: model.StatusReady}}
	r := gin.New()
	NewInternalLifecycleHandler(store, service.NewReconciler(kc, nil), nil, "internal").RegisterRoutes(r)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/internal/v1/sandboxes/71/complete", nil)
		req.Header.Set("X-Internal-Token", "internal")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		bindings, err := cs.RbacV1().RoleBindings("sandbox-71").List(ctx, meta.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range bindings.Items {
			if len(b.Subjects) != 0 {
				t.Fatal("complete acknowledged before access revocation")
			}
		}
	}
	if _, err := cs.CoreV1().Namespaces().Get(ctx, "sandbox-71", meta.GetOptions{}); err != nil {
		t.Fatal("destroyed evidence before collection", err)
	}
}
