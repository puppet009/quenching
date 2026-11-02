package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/quenching/sandbox-service/internal/service"
)

// fakeWaiter 需要实现 ProvisionService 兼容的等待器接口
// 我们用一个轻量适配器
type waiterAdapter struct {
	fw *fakeWaiter
}

func (w *waiterAdapter) WaitForDeployments(ctx context.Context, namespace string) (int, error) {
	return w.fw.WaitForDeployments(ctx, namespace)
}

func newProvisionWithFakes(fd *fakeDeployer, fw *fakeWaiter) *service.ProvisionService {
	// ReadyWaiter 是 struct 不是 interface，我们直接测试 Provision + mock Deployer
	// 对于等待器，我们用另一种方式测试：ReadyWaiter 单独测
	_ = fw
	return service.NewProvisionService(fd, nil) // waiter 为 nil 时跳过等待
}

func TestProvisionSuccess(t *testing.T) {
	fd := newFakeDeployer()
	svc := service.NewProvisionService(fd, nil) // 跳过等待

	err := svc.Provision(context.Background(), service.ProvisionOpts{
		Namespace:   "sandbox-123",
		ReleaseName: "ecommerce",
		ChartPath:   "/charts/ecommerce",
	})
	if err != nil {
		t.Fatalf("部署应成功，got err: %v", err)
	}
	if fd.installCount != 1 {
		t.Errorf("Install 应调用 1 次，got %d", fd.installCount)
	}
	exists, _ := fd.Exists(context.Background(), "sandbox-123", "ecommerce")
	if !exists {
		t.Error("部署成功后 Release 应存在")
	}
}

func TestProvisionInstallFailTriggersRollback(t *testing.T) {
	fd := newFakeDeployer()
	fd.installError = errors.New("chart not found")
	svc := service.NewProvisionService(fd, nil)

	err := svc.Provision(context.Background(), service.ProvisionOpts{
		Namespace:   "sandbox-123",
		ReleaseName: "ecommerce",
		ChartPath:   "/charts/nonexistent",
	})
	if err == nil {
		t.Fatal("安装失败应返回错误")
	}
	// 回滚应被调用
	if fd.uninstallCount != 1 {
		t.Errorf("安装失败应触发回滚（Uninstall），调用次数: %d", fd.uninstallCount)
	}
}

func TestProvisionSkipWaiter(t *testing.T) {
	// waiter 为 nil 时应跳过等待，直接返回成功
	fd := newFakeDeployer()
	svc := service.NewProvisionService(fd, nil)

	err := svc.Provision(context.Background(), service.ProvisionOpts{
		Namespace:   "sandbox-456",
		ReleaseName: "test",
		ChartPath:   "/charts/test",
	})
	if err != nil {
		t.Fatalf("waiter 为 nil 时应成功，got err: %v", err)
	}
}

func TestTeardownSuccess(t *testing.T) {
	fd := newFakeDeployer()
	// 先安装
	fd.Install(context.Background(), service.ChartInfo{
		Name:      "ecommerce",
		Namespace: "sandbox-123",
		ChartPath: "/charts/test",
	})

	svc := service.NewProvisionService(fd, nil)
	err := svc.Teardown(context.Background(), "sandbox-123", "ecommerce")
	if err != nil {
		t.Fatalf("卸载应成功，got err: %v", err)
	}
	if fd.uninstallCount != 1 {
		t.Errorf("Uninstall 应调用 1 次，got %d", fd.uninstallCount)
	}
}

func TestTeardownIdempotent(t *testing.T) {
	// 卸载不存在的 release 不应报错
	fd := newFakeDeployer()
	svc := service.NewProvisionService(fd, nil)

	err := svc.Teardown(context.Background(), "sandbox-999", "nonexistent")
	if err != nil {
		t.Fatalf("卸载不存在的 release 应成功（幂等），got err: %v", err)
	}
}

func TestFakeDeployerExists(t *testing.T) {
	fd := newFakeDeployer()

	exists, _ := fd.Exists(context.Background(), "ns", "test")
	if exists {
		t.Error("初始状态应不存在")
	}

	fd.Install(context.Background(), service.ChartInfo{
		Name:      "test",
		Namespace: "ns",
		ChartPath: "/test",
	})

	exists, _ = fd.Exists(context.Background(), "ns", "test")
	if !exists {
		t.Error("安装后应存在")
	}
}
