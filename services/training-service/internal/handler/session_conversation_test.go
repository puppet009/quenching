package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"net/http/httptest"
	"testing"
)

type restoreHandlerStore struct{ service.SessionMemoryStore }

func (*restoreHandlerStore) Resolve(_ context.Context, uid, sid int64) (*model.SessionConversation, error) {
	if uid != 7 || sid != 1 {
		return nil, service.ErrSessionMemoryNotFound
	}
	return &model.SessionConversation{ConversationID: 9, SessionID: 1, SessionStatus: "completed"}, nil
}
func (*restoreHandlerStore) History(_ context.Context, uid, sid, before int64, n int) (model.SessionHistoryPage, error) {
	return model.SessionHistoryPage{Messages: []model.SessionMessage{{ID: 1, Role: "user", Content: "before refresh"}}}, nil
}
func TestSessionRestoreHTTPAuthorizationAndCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewChatService(newMemStore(), nil, "qwen3.8-flash", "")
	svc.WithSessionMemory(&restoreHandlerStore{})
	router := gin.New()
	NewConversationHandler(svc).RegisterRoutes(router)
	for _, tc := range []struct {
		path, user string
		want       int
	}{{"/api/v1/ai/sessions/1/conversation", "7", 200}, {"/api/v1/ai/sessions/1/conversation", "8", 404}, {"/api/v1/ai/sessions/0/conversation", "7", 400}, {"/api/v1/ai/sessions/1/conversation", "", 401}, {"/api/v1/ai/sessions/1/conversation/messages?before_id=-1", "7", 400}, {"/api/v1/ai/sessions/1/conversation/messages?limit=101", "7", 400}} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", tc.path, nil)
		if tc.user != "" {
			req.Header.Set("X-User-Id", tc.user)
		}
		router.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
