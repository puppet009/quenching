package tests

import (
	"context"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// newTestSandboxService 用默认（资源充足）配置构造 SandboxService。
func newTestSandboxService() (*service.SandboxService, *fakeStore) {
	return newTestSandboxServiceWithResource(true, nil)
}

// newTestSandboxServiceWithResource 可控资源检查结果地构造 SandboxService。
func newTestSandboxServiceWithResource(enough bool, resErr error) (*service.SandboxService, *fakeStore) {
	cs := fake.NewSimpleClientset()
	kc := service.NewK8sClient(cs)
	store := &fakeStore{}
	rec := service.NewReconciler(kc, store)
	quota := service.NewQuotaService(store)
	mockRes := &mockResourceChecker{enough: enough, err: resErr}
	svc := service.NewSandboxService(rec, quota, mockRes, store, nil, nil)
	return svc, store
}

// TestCreateSandbox_NewUserSuccess 新用户创建沙箱应成功。
func TestCreateSandbox_NewUserSuccess(t *testing.T) {
	svc, store := newTestSandboxService()
	ctx := context.Background()

	sb, err := svc.CreateSandbox(ctx, 1001, 2001, 3001, "")
	if err != nil {
		t.Fatalf("新用户创建沙箱应成功，实际报错: %v", err)
	}
	if sb == nil {
		t.Fatal("返回的沙箱不应为 nil")
	}
	if sb.UserID != 1001 {
		t.Errorf("UserID 不匹配, got %d want 1001", sb.UserID)
	}
	if sb.Status != model.StatusDeploying {
		t.Errorf("创建后状态应为 deploying, got %s", sb.Status)
	}
	if sb.Namespace != "sandbox-3001" {
		t.Errorf("Namespace 不匹配, got %s", sb.Namespace)
	}

	// 验证 DB 里有记录
	count, _ := store.CountActiveByUser(ctx, 1001)
	if count != 1 {
		t.Errorf("DB 中用户活跃沙箱数应为 1, 实际 %d", count)
	}
}

// TestCreateSandbox_QuotaExceeded 配额超限应被拦截，不会创建 DB 记录。
func TestCreateSandbox_QuotaExceeded(t *testing.T) {
	svc, store := newTestSandboxService()
	ctx := context.Background()

	// 先创建一个沙箱（达到上限 1）
	_, _ = svc.CreateSandbox(ctx, 2001, 1001, 5001, "")

	// 再创建第二个，应被拒
	_, err := svc.CreateSandbox(ctx, 2001, 1001, 5002, "")
	if err == nil {
		t.Fatal("配额超限应被拒绝，实际成功了")
	}
	if !strings.Contains(err.Error(), "配额超限") {
		t.Errorf("错误消息应包含'配额超限', 实际: %s", err.Error())
	}

	// 验证第二个沙箱没有被创建
	count, _ := store.CountActiveByUser(ctx, 2001)
	if count != 1 {
		t.Errorf("配额超限的情况下不应创建第二条记录，实际有 %d 条", count)
	}
}

// TestCreateSandbox_DifferentUsersIndependent 不同用户配额互不影响。
func TestCreateSandbox_DifferentUsersIndependent(t *testing.T) {
	svc, store := newTestSandboxService()
	ctx := context.Background()

	// 用户 A 有 1 个沙箱
	_, _ = svc.CreateSandbox(ctx, 3001, 1001, 6001, "")

	// 用户 B 也创建 1 个，应成功
	sb, err := svc.CreateSandbox(ctx, 3002, 1001, 6002, "")
	if err != nil {
		t.Fatalf("用户B不应受用户A配额影响，实际报错: %v", err)
	}
	if sb == nil || sb.UserID != 3002 {
		t.Error("返回的沙箱不正确")
	}

	// 验证各自都有 1 条
	ac, _ := store.CountActiveByUser(ctx, 3001)
	bc, _ := store.CountActiveByUser(ctx, 3002)
	if ac != 1 || bc != 1 {
		t.Errorf("两个用户应各有 1 个沙箱，实际 A=%d B=%d", ac, bc)
	}
}

// TestCreateSandbox_InsufficientResource 集群资源不足应友好报错。
func TestCreateSandbox_InsufficientResource(t *testing.T) {
	svc, store := newTestSandboxServiceWithResource(false, nil)
	ctx := context.Background()

	_, err := svc.CreateSandbox(ctx, 4001, 1001, 7001, "")
	if err == nil {
		t.Fatal("资源不足应被拒绝，实际成功了")
	}
	if !strings.Contains(err.Error(), "资源不足") {
		t.Errorf("错误消息应包含'资源不足', 实际: %s", err.Error())
	}

	// 验证沙箱没有被创建（不会走到配额校验和落库）
	count, _ := store.CountActiveByUser(ctx, 4001)
	if count != 0 {
		t.Errorf("资源不足时不应创建沙箱记录，实际有 %d 条", count)
	}
}

// TestCreateSandbox_ResourceCheckErrorFallback 资源检查出错时降级放行，不阻塞创建。
func TestCreateSandbox_ResourceCheckErrorFallback(t *testing.T) {
	svc, _ := newTestSandboxServiceWithResource(false, context.DeadlineExceeded)
	ctx := context.Background()

	// 即使资源检查报错，也应降级放行，让 kube-scheduler 兜底
	sb, err := svc.CreateSandbox(ctx, 5001, 1001, 8001, "")
	if err != nil {
		t.Fatalf("资源检查报错应降级放行，实际报错: %v", err)
	}
	if sb == nil {
		t.Fatal("应成功创建沙箱")
	}
}

// TestCreateSandbox_NoResourceChecker 没有资源检查器时应正常工作。
func TestCreateSandbox_NoResourceChecker(t *testing.T) {
	cs := fake.NewSimpleClientset()
	kc := service.NewK8sClient(cs)
	store := &fakeStore{}
	rec := service.NewReconciler(kc, store)
	quota := service.NewQuotaService(store)
	// resource 传 nil，模拟无集群环境；provision=nil 本地降级，registry 传 nil 用空注册表
	svc := service.NewSandboxService(rec, quota, nil, store, nil, nil)

	ctx := context.Background()
	sb, err := svc.CreateSandbox(ctx, 6001, 1001, 9001, "")
	if err != nil {
		t.Fatalf("无资源检查器时应正常创建，实际报错: %v", err)
	}
	if sb == nil {
		t.Fatal("应成功创建沙箱")
	}
}
