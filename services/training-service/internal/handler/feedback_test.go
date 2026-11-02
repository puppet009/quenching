package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/quenching/training-service/internal/service"
	"net/http/httptest"
	"strings"
	"testing"
)

type feedbackTestStore struct {
	calls int
	last  service.FeedbackInput
}

func (s *feedbackTestStore) Save(_ context.Context, uid int64, in service.FeedbackInput) (int64, error) {
	s.calls++
	if uid != 7 || in.TrainingID != 42 {
		return 0, pgx.ErrNoRows
	}
	s.last = in
	return 9, nil
}
func (*feedbackTestStore) List(context.Context, int, int, int, int, *bool) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func TestFeedbackHTTPValidationOwnershipAndRepeat(t *testing.T) {
	store := &feedbackTestStore{}
	r := gin.New()
	NewFeedbackHandler(store).RegisterRoutes(r)
	cases := []struct {
		path, body, uid string
		status          int
	}{
		{"nps", `{"training_id":42,"score":0}`, "7", 200},
		{"nps", `{"training_id":42,"score":10}`, "7", 200},
		{"nps", `{"training_id":42,"score":11}`, "7", 400},
		{"nps", `{"training_id":42}`, "7", 400},
		{"nps", `{"training_id":42,"score":5}`, "8", 404},
		{"nps", `{"training_id":42,"score":5}`, "", 401},
		{"value", `{"training_id":42,"value":"valuable"}`, "7", 200},
		{"value", `{"training_id":42,"value":"bad"}`, "7", 400},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/api/v1/feedback/"+c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", c.uid)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != c.status {
			t.Fatalf("%+v: %d %s", c, w.Code, w.Body.String())
		}
	}
	if store.calls != 4 {
		t.Fatalf("invalid input reached store: %d", store.calls)
	}
	if store.last.ValueFeedback == nil || *store.last.ValueFeedback != "valuable" {
		t.Fatal("quick feedback alias lost")
	}
}
