// T4-16 复盘历史接口测试：service 分页/uid 过滤 + handler HTTP 接线。
// 纯内存 fake + httptest，不连数据库/RabbitMQ，可离线运行。
package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/quenching/review-service/internal/handler"
	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
)

// seedHistory 造 ownerUID 的 n 条 + otherUID 的 1 条复盘（不同会话）。
func seedHistory(t *testing.T, svc *service.ReviewService, ownerUID, otherUID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := svc.StartGeneration(context.Background(), int64(1000+i), ownerUID, 1); err != nil {
			t.Fatalf("造数失败: %v", err)
		}
	}
	if _, err := svc.StartGeneration(context.Background(), 9000, otherUID, 1); err != nil {
		t.Fatalf("造他人数据失败: %v", err)
	}
}

// newHistoryRouter 构造挂载 history/status 两路由的测试引擎；
// authUID>=0 时注入模拟登录态，<0 表示不注入（模拟未登录）。
func newHistoryRouter(svc *service.ReviewService, authUID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if authUID >= 0 {
		r.Use(func(c *gin.Context) {
			c.Set("uid", authUID)
			c.Next()
		})
	}
	h := handler.NewReviewHandler(svc)
	r.GET("/history", h.ListHistory)
	r.GET("/session/:session_id", h.GetBySession)
	return r
}

// ---------- service 层 ----------

// TestListHistory_OnlyOwnUID 历史只返回当前 uid 的复盘（IDOR 统一治理留 T5-6）。
func TestListHistory_OnlyOwnUID(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})
	seedHistory(t, svc, 101, 202, 3)

	page, err := svc.ListHistory(context.Background(), 101, 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 3 {
		t.Errorf("自己的复盘总数应为 3，got %d", page.Total)
	}
	if len(page.List) != 3 {
		t.Errorf("应返回 3 条，got %d", len(page.List))
	}
	for _, item := range page.List {
		if item.SessionID == 9000 {
			t.Error("历史中混入了其他用户的复盘")
		}
	}

	other, err := svc.ListHistory(context.Background(), 202, 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if other.Total != 1 || len(other.List) != 1 || other.List[0].SessionID != 9000 {
		t.Errorf("uid=202 应只看到自己的 1 条，got %+v", other)
	}
}

// TestListHistory_Pagination 翻页与参数钳制在 service 层的表现。
func TestListHistory_Pagination(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})
	seedHistory(t, svc, 101, 202, 3)

	first, _ := svc.ListHistory(context.Background(), 101, 1, 2)
	if len(first.List) != 2 || first.Total != 3 {
		t.Fatalf("第 1 页 size=2 应有 2 条/total=3，got %d/%d", len(first.List), first.Total)
	}
	second, _ := svc.ListHistory(context.Background(), 101, 2, 2)
	if len(second.List) != 1 {
		t.Fatalf("第 2 页应剩 1 条，got %d", len(second.List))
	}
	// 两页 ID 不重叠
	if first.List[0].ID == second.List[0].ID || first.List[1].ID == second.List[0].ID {
		t.Error("两页出现重复复盘")
	}

	// 非法参数（0/超大）经 NormalizePage 钳制后不报错
	clamped, err := svc.ListHistory(context.Background(), 101, 0, 0)
	if err != nil {
		t.Fatalf("非法分页参数应被钳制而非报错: %v", err)
	}
	if len(clamped.List) != 3 {
		t.Errorf("默认参数下应返回全部 3 条，got %d", len(clamped.List))
	}
	big, _ := svc.ListHistory(context.Background(), 101, 1, 9999)
	if len(big.List) != 3 {
		t.Errorf("size 超上限钳到 100，仍应只有 3 条数据，got %d", len(big.List))
	}
}

// TestListHistory_EmptyUser 无复盘用户返回空列表（非 nil），total=0 且不查明细。
func TestListHistory_EmptyUser(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})

	page, err := svc.ListHistory(context.Background(), 777, 1, 20)
	if err != nil {
		t.Fatalf("空用户查询不应报错: %v", err)
	}
	if page.Total != 0 || len(page.List) != 0 {
		t.Errorf("空用户应为 total=0/空列表，got %d/%d", page.Total, len(page.List))
	}
	if page.List == nil {
		t.Error("空列表应为非 nil 空切片，保证前端 JSON 序列化为 []")
	}
}

// TestGetStatus_Lightweight 状态轮询只回状态字段，不存在返回 ErrNotFound。
func TestGetStatus_Lightweight(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})
	seedHistory(t, svc, 101, 202, 1)

	st, err := svc.GetStatus(context.Background(), 1000)
	if err != nil {
		t.Fatalf("已创建会话应能查到状态: %v", err)
	}
	if st.GenerationStatus != model.GenStatusGenerating {
		t.Errorf("投递成功后状态应为 generating，got %s", st.GenerationStatus)
	}
	if st.SessionID != 1000 {
		t.Errorf("session_id 不匹配: %d", st.SessionID)
	}

	if _, err := svc.GetStatus(context.Background(), 4321); err != service.ErrNotFound {
		t.Errorf("不存在的会话应返回 ErrNotFound，got %v", err)
	}
}

// ---------- handler 层（httptest） ----------

type historyResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total int64 `json:"total"`
		List  []struct {
			ID        int64  `json:"id"`
			SessionID int64  `json:"session_id"`
			Status    string `json:"generation_status"`
		} `json:"list"`
	} `json:"data"`
}

// TestHistoryHandler_DefaultAndPaging HTTP 层：默认参数、显式翻页、非法值回默认。
func TestHistoryHandler_DefaultAndPaging(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})
	seedHistory(t, svc, 101, 202, 3)
	r := newHistoryRouter(svc, 101)

	// 默认分页
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP 状态应为 200，got %d", w.Code)
	}
	var resp historyResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应体非法 JSON: %v; body=%s", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("业务码应为 0，got %d/%s", resp.Code, resp.Msg)
	}
	if resp.Data.Total != 3 || len(resp.Data.List) != 3 {
		t.Errorf("默认分页应得 3 条，got total=%d len=%d", resp.Data.Total, len(resp.Data.List))
	}

	// 显式翻到第 2 页
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history?page=2&size=2", nil))
	resp = historyResp{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data.List) != 1 {
		t.Errorf("第 2 页 size=2 应剩 1 条，got %d", len(resp.Data.List))
	}

	// 非法分页字符串：Atoi 失败得 0，service 钳回默认，不报错
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history?page=abc&size=xyz", nil))
	resp = historyResp{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || len(resp.Data.List) != 3 {
		t.Errorf("非法分页参数应回默认并成功，got code=%d len=%d", resp.Code, len(resp.Data.List))
	}
}

// TestHistoryHandler_UIDFromContext 未登录（无 uid）按 0 查，返回空集而非他人数据。
func TestHistoryHandler_UIDFromContext(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})
	seedHistory(t, svc, 101, 202, 3)
	r := newHistoryRouter(svc, -1) // 不注入 uid 中间件

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/history", nil))
	var resp historyResp
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != 0 || resp.Data.Total != 0 || len(resp.Data.List) != 0 {
		t.Errorf("无 uid 上下文应得空集，got code=%d total=%d", resp.Code, resp.Data.Total)
	}
}

// TestStatusHandler_BySession 状态轮询路由：命中/未命中/非法入参的业务码。
func TestStatusHandler_BySession(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, newFakeProducer(), repo).WithReviewEligibility(growthEligibility{})
	seedHistory(t, svc, 101, 202, 1)
	r := newHistoryRouter(svc, 101)

	// 命中
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session/1000", nil))
	var ok struct {
		Code int `json:"code"`
		Data struct {
			Status    string `json:"generation_status"`
			SessionID int64  `json:"session_id"`
		} `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &ok)
	if ok.Code != 0 || ok.Data.Status != model.GenStatusGenerating || ok.Data.SessionID != 1000 {
		t.Errorf("命中状态不对: %+v body=%s", ok, w.Body.String())
	}

	// 未命中 → 10004
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session/4321", nil))
	var miss struct {
		Code int `json:"code"`
	}
	json.Unmarshal(w.Body.Bytes(), &miss)
	if miss.Code != 10004 {
		t.Errorf("不存在的会话业务码应为 10004，got %d", miss.Code)
	}

	// 非法 session_id → 10001
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/session/not-a-number", nil))
	var bad struct {
		Code int `json:"code"`
	}
	json.Unmarshal(w.Body.Bytes(), &bad)
	if bad.Code != 10001 {
		t.Errorf("非法 session_id 业务码应为 10001，got %d", bad.Code)
	}
}
