// Package tests fault-engine 单测。
// 用 fakeExecutor 在本机（无需集群/ChaosBlade）验证命令构造、注入、恢复全流程。
package tests

import (
	"context"
	"fmt"
	"sync"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
)

// fakeExecutor 记录每次执行的命令，并按自增序号返回 blade 风格 JSON。
type fakeExecutor struct {
	mu       sync.Mutex
	calls    []fakeCall
	uidSeq   int
	failNext bool
}

type fakeCall struct {
	spec model.InjectSpec
	args []string
}

func (f *fakeExecutor) Run(_ context.Context, spec model.InjectSpec, args []string) (*service.BladeExecResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, fakeCall{spec: spec, args: append([]string{}, args...)})
	if f.failNext {
		f.failNext = false
		return nil, fmt.Errorf("模拟执行失败")
	}
	// destroy/status 不需要返回 uid JSON，直接给成功空串
	if len(args) > 0 && (args[0] == "destroy" || args[0] == "status") {
		return &service.BladeExecResult{Raw: `{"code":200,"success":true,"result":""}`}, nil
	}
	f.uidSeq++
	uid := fmt.Sprintf("fake-uid-%d", f.uidSeq)
	raw := fmt.Sprintf(`{"code":200,"success":true,"result":"%s"}`, uid)
	return &service.BladeExecResult{ExperimentUID: uid, Raw: raw}, nil
}

func (f *fakeExecutor) lastArgs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1].args
}

// CallsSnapshot 返回全部执行记录的快照（供运行时服务测试断言调用次数）。
func (f *fakeExecutor) CallsSnapshot() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall{}, f.calls...)
}

// FailNextOnce 令下一次 Run 返回失败（仅生效一次）。
func (f *fakeExecutor) FailNextOnce() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = true
}
