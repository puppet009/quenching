// 会话结束撤销钩子（finish）单测：验证双路撤销调用顺序、失败聚合与参数校验。
package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// fakeRevoker 记录撤销调用并可注入错误。
type fakeRevoker struct {
	calledWith []int64
	err        error
}

func (f *fakeRevoker) RevokeSessionConsequences(_ context.Context, sessionID int64) error {
	f.calledWith = append(f.calledWith, sessionID)
	return f.err
}

// fakeStopper 记录停止调用并可注入错误。
type fakeStopper struct {
	calledWith []int64
	err        error
}

func (f *fakeStopper) StopSession(_ context.Context, sessionID int64) error {
	f.calledWith = append(f.calledWith, sessionID)
	return f.err
}

// fakeSessionScorer 记录结束评分调用并可注入结果/错误。
type fakeSessionScorer struct {
	calls  []fakeScoreCall
	result *service.FinishSessionResult
	err    error
}

type fakeScoreCall struct {
	sessionID int64
	outcome   service.FinishOutcome
}

func (f *fakeSessionScorer) FinishSessionScore(_ context.Context, sessionID int64, outcome service.FinishOutcome) (*service.FinishSessionResult, error) {
	f.calls = append(f.calls, fakeScoreCall{sessionID: sessionID, outcome: outcome})
	if f.err != nil {
		return nil, f.err
	}
	if f.result == nil {
		return &service.FinishSessionResult{Scored: true}, nil
	}
	return f.result, nil
}

func newFinishRouter(h *RuntimeHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterRoutes(r)
	return r
}

func postFinish(r *gin.Engine, id string) *httptest.ResponseRecorder {
	return postFinishWithBody(r, id, "")
}

func postFinishWithBody(r *gin.Engine, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/runtime/sessions/"+id+"/finish", strings.NewReader(body))
	r.ServeHTTP(w, req)
	return w
}

func TestRuntimeFinishSuccess(t *testing.T) {
	rev := &fakeRevoker{}
	stp := &fakeStopper{}
	r := newFinishRouter(NewRuntimeHandler(rev, stp))

	w := postFinish(r, "123")
	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	if len(rev.calledWith) != 1 || rev.calledWith[0] != 123 {
		t.Fatalf("次生故障撤销应按会话ID调用一次，实际 %v", rev.calledWith)
	}
	if len(stp.calledWith) != 1 || stp.calledWith[0] != 123 {
		t.Fatalf("剧本编排停止应按会话ID调用一次，实际 %v", stp.calledWith)
	}
}

func TestRuntimeFinishNilStopper(t *testing.T) {
	// fault-engine 未配置：stopper 为 nil，仅撤次生故障也应成功。
	rev := &fakeRevoker{}
	r := newFinishRouter(NewRuntimeHandler(rev, nil))

	w := postFinish(r, "5")
	if w.Code != http.StatusOK {
		t.Fatalf("stopper 为 nil 时应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	if len(rev.calledWith) != 1 {
		t.Fatal("次生故障撤销仍应被调用")
	}
}

func TestRuntimeFinishRevokerErrorAggregated(t *testing.T) {
	rev := &fakeRevoker{err: errors.New("blade destroy 超时")}
	stp := &fakeStopper{}
	r := newFinishRouter(NewRuntimeHandler(rev, stp))

	w := postFinish(r, "9")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("撤销失败应返回 500，实际 %d", w.Code)
	}
	body := w.Body.String()
	if !contains(body, "会话故障撤销未完全成功") || !contains(body, "blade destroy 超时") {
		t.Fatalf("错误响应应聚合原因，实际 %s", body)
	}
	// 即便第一路失败，第二路仍须尝试。
	if len(stp.calledWith) != 1 {
		t.Fatal("次生撤销失败不应阻断剧本编排停止")
	}
}

func TestRuntimeFinishStopperErrorAggregated(t *testing.T) {
	rev := &fakeRevoker{}
	stp := &fakeStopper{err: errors.New("引擎不可达")}
	r := newFinishRouter(NewRuntimeHandler(rev, stp))

	w := postFinish(r, "9")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("停止失败应返回 500，实际 %d", w.Code)
	}
	if !contains(w.Body.String(), "引擎不可达") {
		t.Fatalf("错误信息应透传，实际 %s", w.Body.String())
	}
}

func TestRuntimeFinishInvalidID(t *testing.T) {
	rev := &fakeRevoker{}
	stp := &fakeStopper{}
	r := newFinishRouter(NewRuntimeHandler(rev, stp))

	for _, id := range []string{"abc", "0", "-3"} {
		w := postFinish(r, id)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("非法ID %q 应返回 400，实际 %d", id, w.Code)
		}
		if !contains(w.Body.String(), "会话ID非法") {
			t.Fatalf("非法ID %q 应提示会话ID非法，实际 %s", id, w.Body.String())
		}
	}
	if len(rev.calledWith) != 0 || len(stp.calledWith) != 0 {
		t.Fatal("参数非法时不应触发任何撤销动作")
	}
}

// ——— 最小闭环（i3d）：撤销成功后的尽力评分 ———

// 撤销成功后评分器被调用，无 body 时 outcome 为 Unspecified。
func TestRuntimeFinishScorerInvokedAfterSuccess(t *testing.T) {
	rev := &fakeRevoker{}
	stp := &fakeStopper{}
	scr := &fakeSessionScorer{}
	h := NewRuntimeHandler(rev, stp).SetSessionScorer(scr)
	r := newFinishRouter(h)

	w := postFinish(r, "123")
	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	if len(scr.calls) != 1 {
		t.Fatalf("撤销成功后评分应被调用一次，实际 %d", len(scr.calls))
	}
	got := scr.calls[0]
	if got.sessionID != 123 || got.outcome != service.FinishOutcomeUnspecified {
		t.Fatalf("评分入参不符，实际 %+v", got)
	}
}

// body 显式声明 outcome=completed 时应透传给评分器。
func TestRuntimeFinishScorerParsesOutcome(t *testing.T) {
	scr := &fakeSessionScorer{}
	h := NewRuntimeHandler(&fakeRevoker{}, nil).SetSessionScorer(scr)
	r := newFinishRouter(h)

	w := postFinishWithBody(r, "7", `{"outcome":"completed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", w.Code)
	}
	if len(scr.calls) != 1 || scr.calls[0].outcome != service.FinishOutcomeCompleted {
		t.Fatalf("应透传 completed，实际 %+v", scr.calls)
	}
}

// body 非法 JSON 时不报错，outcome 回落为 Unspecified。
func TestRuntimeFinishScorerMalformedBody(t *testing.T) {
	scr := &fakeSessionScorer{}
	h := NewRuntimeHandler(&fakeRevoker{}, nil).SetSessionScorer(scr)
	r := newFinishRouter(h)

	w := postFinishWithBody(r, "7", `{not-json`)
	if w.Code != http.StatusOK {
		t.Fatalf("非法 body 不应影响撤销成功响应，实际 %d", w.Code)
	}
	if len(scr.calls) != 1 || scr.calls[0].outcome != service.FinishOutcomeUnspecified {
		t.Fatalf("非法 body 应回落 Unspecified，实际 %+v", scr.calls)
	}
}

// 撤销任一路失败时不评分（云端重试撤销成功后再评）。
func TestRuntimeFinishScorerSkippedOnRevokeError(t *testing.T) {
	rev := &fakeRevoker{err: errors.New("blade destroy 超时")}
	stp := &fakeStopper{}
	scr := &fakeSessionScorer{}
	h := NewRuntimeHandler(rev, stp).SetSessionScorer(scr)
	r := newFinishRouter(h)

	w := postFinish(r, "9")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("撤销失败应返回 500，实际 %d", w.Code)
	}
	if len(scr.calls) != 0 {
		t.Fatalf("撤销失败时不应评分，实际调用 %d 次", len(scr.calls))
	}
}

// 会话ID非法时不评分。
func TestRuntimeFinishScorerSkippedOnInvalidID(t *testing.T) {
	scr := &fakeSessionScorer{}
	h := NewRuntimeHandler(&fakeRevoker{}, &fakeStopper{}).SetSessionScorer(scr)
	r := newFinishRouter(h)

	w := postFinish(r, "0")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法ID应返回 400，实际 %d", w.Code)
	}
	if len(scr.calls) != 0 {
		t.Fatal("非法ID时不应评分")
	}
}

// 评分本身失败时撤销响应仍为 200（尽力而为，只记日志）。
func TestRuntimeFinishScorerErrorDoesNotAffectResponse(t *testing.T) {
	scr := &fakeSessionScorer{err: errors.New("ai-service 不可达")}
	h := NewRuntimeHandler(&fakeRevoker{}, nil).SetSessionScorer(scr)
	r := newFinishRouter(h)

	w := postFinish(r, "21")
	if w.Code != http.StatusOK {
		t.Fatalf("评分失败不应影响撤销响应码，实际 %d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !contains(body, `"code":0`) {
		t.Fatalf("响应体应保持撤销成功结构，实际 %s", body)
	}
}

// 未装配评分器（nil）时行为与旧版完全一致：既不 panic 也不评分。
func TestRuntimeFinishNilScorer(t *testing.T) {
	rev := &fakeRevoker{}
	stp := &fakeStopper{}
	h := NewRuntimeHandler(rev, stp) // 不调用 SetSessionScorer
	r := newFinishRouter(h)

	w := postFinish(r, "3")
	if w.Code != http.StatusOK {
		t.Fatalf("未装配评分器应保持 200，实际 %d", w.Code)
	}
	if len(rev.calledWith) != 1 || len(stp.calledWith) != 1 {
		t.Fatal("双路撤销应照常各调用一次")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
