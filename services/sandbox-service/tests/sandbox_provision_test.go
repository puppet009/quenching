package tests

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// ---------- 测试用例 ----------

// TestCreateSandboxWithProvision 测试带部署器的完整创建流程。
func TestCreateSandboxWithProvision(t *testing.T) {
	store := newFakeStore()
	rec := service.NewReconciler(service.NewK8sClient(fake.NewSimpleClientset()), store)
	quota := service.NewQuotaService(store)
	fd := newFakeDeployer()
	provision := service.NewProvisionService(fd, nil) // 跳过等待
	reg := newFakeRegistry("mall")

	svc := service.NewSandboxService(rec, quota, nil, store, provision, reg)

	sb, err := svc.CreateSandbox(context.Background(), 1001, 1, 10001, "mall")
	if err != nil {
		t.Fatalf("创建沙箱应成功，got err: %v", err)
	}

	// 状态应为 ready（部署成功）
	if sb.Status != model.StatusReady {
		t.Errorf("部署成功后状态应为 ready，got %s", sb.Status)
	}

	// Helm 应已安装
	if fd.installCount != 1 {
		t.Errorf("Helm Install 应调用 1 次，got %d", fd.installCount)
	}
	exists, _ := fd.Exists(context.Background(), sb.Namespace, "mall")
	if !exists {
		t.Error("Helm Release 应存在，release 名应等于模板 slug")
	}
}

// TestCreateSandboxProvisionFailMarksFailed 测试部署失败时标记 failed 并回滚。
func TestCreateSandboxProvisionFailMarksFailed(t *testing.T) {
	store := newFakeStore()
	rec := service.NewReconciler(service.NewK8sClient(fake.NewSimpleClientset()), store)
	quota := service.NewQuotaService(store)
	fd := newFakeDeployer()
	fd.installError = errFakeInstallFail
	provision := service.NewProvisionService(fd, nil)
	reg := newFakeRegistry("mall")

	svc := service.NewSandboxService(rec, quota, nil, store, provision, reg)

	_, err := svc.CreateSandbox(context.Background(), 1001, 1, 10001, "mall")
	if err == nil {
		t.Fatal("部署失败应返回错误")
	}

	// 状态应为 failed
	sb, _ := store.GetBySessionID(context.Background(), 10001)
	if sb == nil {
		t.Fatal("沙箱记录应存在")
	}
	if sb.Status != model.StatusFailed {
		t.Errorf("部署失败后状态应为 failed，got %s", sb.Status)
	}

	// 回滚应被调用
	if fd.uninstallCount != 1 {
		t.Errorf("部署失败应触发回滚，Uninstall 调用次数: %d", fd.uninstallCount)
	}
}

// TestCreateSandboxWithoutProvision 测试无部署器时只到 deploying 状态。
func TestCreateSandboxWithoutProvision(t *testing.T) {
	store := newFakeStore()
	rec := service.NewReconciler(service.NewK8sClient(fake.NewSimpleClientset()), store)
	quota := service.NewQuotaService(store)
	reg := newFakeRegistry("mall")

	svc := service.NewSandboxService(rec, quota, nil, store, nil, reg)

	sb, err := svc.CreateSandbox(context.Background(), 1001, 1, 10001, "mall")
	if err != nil {
		t.Fatalf("创建沙箱应成功，got err: %v", err)
	}

	// 无部署器时状态应为 deploying（不往下走）
	if sb.Status != model.StatusDeploying {
		t.Errorf("无部署器时状态应为 deploying，got %s", sb.Status)
	}
}

// TestCreateSandboxQuotaExceededNoDeploy 测试配额超限不触发部署。
func TestCreateSandboxQuotaExceededNoDeploy(t *testing.T) {
	store := newFakeStore()
	rec := service.NewReconciler(service.NewK8sClient(fake.NewSimpleClientset()), store)
	quota := service.NewQuotaService(store)
	fd := newFakeDeployer()
	provision := service.NewProvisionService(fd, nil)
	reg := newFakeRegistry("mall")

	svc := service.NewSandboxService(rec, quota, nil, store, provision, reg)

	// 第一个沙箱
	_, err := svc.CreateSandbox(context.Background(), 1001, 1, 10001, "mall")
	if err != nil {
		t.Fatalf("第一个沙箱应成功，got err: %v", err)
	}

	// 第二个沙箱（超限）
	_, err = svc.CreateSandbox(context.Background(), 1001, 1, 10002, "mall")
	if err == nil {
		t.Fatal("超限应返回错误")
	}

	// Install 应只调用了 1 次（第一个沙箱）
	if fd.installCount != 1 {
		t.Errorf("超限不应触发部署，Install 调用次数: %d", fd.installCount)
	}
}

// TestCreateSandboxUnknownTemplateRejected 测试指定不存在的模板时在落库前拒绝。
func TestCreateSandboxUnknownTemplateRejected(t *testing.T) {
	store := newFakeStore()
	rec := service.NewReconciler(service.NewK8sClient(fake.NewSimpleClientset()), store)
	quota := service.NewQuotaService(store)
	fd := newFakeDeployer()
	provision := service.NewProvisionService(fd, nil)
	reg := newFakeRegistry("mall")

	svc := service.NewSandboxService(rec, quota, nil, store, provision, reg)

	_, err := svc.CreateSandbox(context.Background(), 1001, 1, 10001, "nope")
	if err != service.ErrSandboxTemplateNotFound {
		t.Fatalf("未知模板应返回 ErrSandboxTemplateNotFound，got: %v", err)
	}

	// 不应触发任何部署，也不应落库
	if fd.installCount != 0 {
		t.Errorf("未知模板不应触发部署，Install 调用次数: %d", fd.installCount)
	}
	count, _ := store.CountActiveByUser(context.Background(), 1001)
	if count != 0 {
		t.Errorf("未知模板不应创建沙箱记录，实际有 %d 条", count)
	}
}

// TestCreateSandboxBareSandboxSkipsDeploy 测试不指定模板时跳过部署直接 ready。
func TestCreateSandboxBareSandboxSkipsDeploy(t *testing.T) {
	store := newFakeStore()
	rec := service.NewReconciler(service.NewK8sClient(fake.NewSimpleClientset()), store)
	quota := service.NewQuotaService(store)
	fd := newFakeDeployer()
	provision := service.NewProvisionService(fd, nil)
	reg := newFakeRegistry("mall")

	svc := service.NewSandboxService(rec, quota, nil, store, provision, reg)

	sb, err := svc.CreateSandbox(context.Background(), 1001, 1, 10001, "")
	if err != nil {
		t.Fatalf("裸沙箱创建应成功，got err: %v", err)
	}
	if sb.Status != model.StatusReady {
		t.Errorf("裸沙箱应跳过部署直接 ready，got %s", sb.Status)
	}
	if fd.installCount != 0 {
		t.Errorf("裸沙箱不应触发部署，Install 调用次数: %d", fd.installCount)
	}
}
