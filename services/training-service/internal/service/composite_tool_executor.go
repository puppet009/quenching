// composite_tool_executor.go 组合式工具执行器：按工具名分发到具体处理器。
//
// 设计目的：随里程碑推进，观察类工具会不断增加（日志/指标/告警/拓扑/文件…）。
// ChatService 只依赖单个 ToolExecutor；本结构把多个工具处理器注册在一张表里，
// 调用时按 call.Name 路由，未注册工具返回错误。新增工具只需 Register，
// 编排层（Agent Loop 与步骤事件）无需改动。
package service

import (
	"context"
	"fmt"
	"sort"
)

// ToolHandler 单个工具的执行函数。
type ToolHandler func(ctx context.Context, sessionID int64, call ToolCall) (string, error)

// CompositeToolExecutor 按名分发的组合执行器。
type CompositeToolExecutor struct {
	handlers map[string]ToolHandler
}

// 编译期断言：满足 ToolExecutor。
var _ ToolExecutor = (*CompositeToolExecutor)(nil)

// NewCompositeToolExecutor 创建空组合执行器。
func NewCompositeToolExecutor() *CompositeToolExecutor {
	return &CompositeToolExecutor{handlers: map[string]ToolHandler{}}
}

// Register 注册（或覆盖）一个工具处理器。
func (c *CompositeToolExecutor) Register(name string, h ToolHandler) {
	if name == "" || h == nil {
		return
	}
	c.handlers[name] = h
}

// Has 是否注册了某工具。
func (c *CompositeToolExecutor) Has(name string) bool {
	_, ok := c.handlers[name]
	return ok
}

// Execute 按 call.Name 路由到对应处理器。
func (c *CompositeToolExecutor) Execute(
	ctx context.Context, sessionID int64, call ToolCall,
) (string, error) {
	h, ok := c.handlers[call.Name]
	if !ok {
		return "", fmt.Errorf("未注册的工具: %s", call.Name)
	}
	return h(ctx, sessionID, call)
}

// Names 已注册工具名（排序，主要用于测试/诊断）。
func (c *CompositeToolExecutor) Names() []string {
	names := make([]string, 0, len(c.handlers))
	for n := range c.handlers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
