// behavior_flow_test.go 行为流配置转换、ConfigMap 与信号汇聚测试（行为模拟第二批）。
package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// newSpecFlow 构造一个最小合法的行为流声明（含入口服务已填充的状态，
// 模拟注册表 validateBehaviorFlow 之后的结果）。
func newSpecFlow() *templates.BehaviorFlowSpec {
	return &templates.BehaviorFlowSpec{
		Name:            "test-steady",
		EntryService:    "frontend",
		EntryPath:       "/",
		IntervalSeconds: 5,
		TimeoutSeconds:  15,
		Steps: []templates.BehaviorStep{
			{Name: "goto_home", Action: "goto", URL: "/"},
			{Name: "assert_title", Action: "assert", Selector: "#product-name", Expect: "text", Value: "键盘"},
		},
	}
}

// TestBuildBehaviorFlowConfig 验证声明到运行配置的转换：
// 集群内入口地址、秒转毫秒、步骤拷贝均正确。
func TestBuildBehaviorFlowConfig(t *testing.T) {
	namespace := model.NamespaceName(100)
	services := []templates.ServiceSpec{
		{Name: "frontend", Port: 80, Role: templates.RoleBusiness},
	}

	flow, err := service.BuildBehaviorFlowConfig(namespace, newSpecFlow(), services)
	if err != nil {
		t.Fatalf("构建运行配置失败: %v", err)
	}

	wantURL := "http://frontend." + namespace + ".svc:80/"
	if flow.EntryURL != wantURL {
		t.Fatalf("入口地址不符：得到 %s，期望 %s", flow.EntryURL, wantURL)
	}
	if flow.IntervalMS != 5000 {
		t.Fatalf("间隔应为 5000ms，实际 %d", flow.IntervalMS)
	}
	if flow.TimeoutMS != 15000 {
		t.Fatalf("超时应为 15000ms，实际 %d", flow.TimeoutMS)
	}
	if len(flow.Steps) != 2 {
		t.Fatalf("步骤数应为 2，实际 %d", len(flow.Steps))
	}
}

// TestBuildBehaviorFlowConfigPathPrefix 验证 entry_path 缺少前导斜杠时自动补齐。
func TestBuildBehaviorFlowConfigPathPrefix(t *testing.T) {
	namespace := model.NamespaceName(101)
	spec := newSpecFlow()
	spec.EntryPath = "products/list"
	services := []templates.ServiceSpec{{Name: "frontend", Port: 8080, Role: templates.RoleBusiness}}

	flow, err := service.BuildBehaviorFlowConfig(namespace, spec, services)
	if err != nil {
		t.Fatalf("构建运行配置失败: %v", err)
	}
	if !strings.HasSuffix(flow.EntryURL, "/products/list") {
		t.Fatalf("entry_path 应补齐前导斜杠，实际 %s", flow.EntryURL)
	}
}

// TestBuildBehaviorFlowConfigErrors 验证入口服务缺失/端口非法时返回错误，
// 不返回一个不可用的配置。
func TestBuildBehaviorFlowConfigErrors(t *testing.T) {
	namespace := model.NamespaceName(102)

	if _, err := service.BuildBehaviorFlowConfig(namespace, nil, nil); err == nil {
		t.Fatal("空行为流声明应报错")
	}

	spec := newSpecFlow()
	services := []templates.ServiceSpec{{Name: "other", Port: 80}}
	if _, err := service.BuildBehaviorFlowConfig(namespace, spec, services); err == nil {
		t.Fatal("入口服务不存在应报错")
	}

	spec.EntryService = "frontend"
	servicesNoPort := []templates.ServiceSpec{{Name: "frontend", Port: 0}}
	if _, err := service.BuildBehaviorFlowConfig(namespace, spec, servicesNoPort); err == nil {
		t.Fatal("入口服务端口非法应报错")
	}
}

// TestApplyBehaviorConfigMaps 验证两个 ConfigMap 幂等创建：
// runner 脚本 ConfigMap 与 flow 配置 ConfigMap 内容、键名均符合预期；
// 重复 Apply 不报错。
func TestApplyBehaviorConfigMaps(t *testing.T) {
	kc := newBehaviorK8sClient()
	ctx := context.Background()
	namespace := model.NamespaceName(55)

	cfg := &service.BehaviorFlowConfig{
		Name:       "default",
		EntryURL:   "http://frontend." + namespace + ".svc:80/",
		IntervalMS: 5000,
		TimeoutMS:  15000,
		Steps: []templates.BehaviorStep{
			{Name: "goto_home", Action: "goto", URL: "/"},
		},
	}

	if err := kc.ApplyBehaviorConfigMaps(ctx, 55, cfg); err != nil {
		t.Fatalf("首次 Apply 失败: %v", err)
	}
	if err := kc.ApplyBehaviorConfigMaps(ctx, 55, cfg); err != nil {
		t.Fatalf("二次 Apply 应幂等，实际: %v", err)
	}

	cms := kc.Clientset().CoreV1().ConfigMaps(namespace)

	runnerCM, err := cms.Get(ctx, service.BehaviorRunnerConfigMapName(55), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("查询 runner ConfigMap 失败: %v", err)
	}
	if !strings.Contains(runnerCM.Data[service.BehaviorRunnerScriptFile], "playwright") {
		t.Fatal("runner ConfigMap 应包含 runner.js 内容")
	}

	for _, asset := range []string{"command_runner.js", "protocol_runner.js"} {
		if runnerCM.Data[asset] == "" {
			t.Fatalf("missing executable protocol asset %s", asset)
		}
	}
	flowCM, err := cms.Get(ctx, service.BehaviorFlowConfigMapName(55), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("查询 flow ConfigMap 失败: %v", err)
	}
	var got service.BehaviorFlowConfig
	if err := json.Unmarshal([]byte(flowCM.Data["flow.json"]), &got); err != nil {
		t.Fatalf("flow.json 不是合法 JSON: %v", err)
	}
	if got.EntryURL != cfg.EntryURL || len(got.Steps) != 1 {
		t.Fatalf("flow.json 内容不符：%+v", got)
	}
}

// TestDeleteBehaviorConfigMaps 验证删除幂等：删除后 ConfigMap 不存在，
// 再次删除不报错。
func TestDeleteBehaviorConfigMaps(t *testing.T) {
	kc := newBehaviorK8sClient()
	ctx := context.Background()
	namespace := model.NamespaceName(66)
	cfg := &service.BehaviorFlowConfig{
		Name:     "default",
		EntryURL: "http://frontend:80",
		Steps:    []templates.BehaviorStep{{Name: "goto_home", Action: "goto", URL: "/"}},
	}

	if err := kc.ApplyBehaviorConfigMaps(ctx, 66, cfg); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if err := kc.DeleteBehaviorConfigMaps(ctx, 66); err != nil {
		t.Fatalf("首次删除失败: %v", err)
	}
	cms := kc.Clientset().CoreV1().ConfigMaps(namespace)
	for _, name := range []string{
		service.BehaviorRunnerConfigMapName(66),
		service.BehaviorFlowConfigMapName(66),
	} {
		if _, err := cms.Get(ctx, name, metav1.GetOptions{}); err == nil {
			t.Fatalf("ConfigMap %s 删除后应不存在", name)
		}
	}
	if err := kc.DeleteBehaviorConfigMaps(ctx, 66); err != nil {
		t.Fatalf("删除不存在的 ConfigMap 应幂等成功，实际: %v", err)
	}
}

// TestBehaviorSignalStoreRing 验证信号汇聚：正常写入、按会话隔离、
// 超容量环形覆盖以及 Clear。
func TestBehaviorSignalStoreRing(t *testing.T) {
	store := service.NewBehaviorSignalStore()

	store.Add(service.BehaviorSignal{SessionID: 1, Type: service.SignalStepFailure, Message: "a"})
	store.Add(service.BehaviorSignal{SandboxID: 1, Type: service.SignalPageError, Message: "b"})
	// 无任何会话ID的信号被丢弃。
	store.Add(service.BehaviorSignal{Type: service.SignalHTTPError, Message: "drop"})

	if len(store.List(1)) != 2 {
		t.Fatalf("会话 1 应有 2 条信号，实际 %d", len(store.List(1)))
	}
	if len(store.List(2)) != 0 {
		t.Fatal("会话 2 应无信号（按会话隔离）")
	}

	// 超容量：写入 250 条，仅保留最近 200 条，且最旧的一条被覆盖。
	for i := 0; i < 250; i++ {
		store.Add(service.BehaviorSignal{SessionID: 9, Type: service.SignalConsoleError, Message: strings.Repeat("x", 1)})
	}
	ring := store.List(9)
	if len(ring) != 200 {
		t.Fatalf("环形缓冲应保留 200 条，实际 %d", len(ring))
	}

	store.Clear(9)
	if len(store.List(9)) != 0 {
		t.Fatal("Clear 后缓冲应为空")
	}
}
