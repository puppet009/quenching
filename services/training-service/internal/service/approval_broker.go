// approval_broker.go 修改类工具的用户审批中介（Agent M4）。
//
// 工具循环在执行需用户确认的操作前，通过 Await 挂起并返回一个审批令牌；
// 前端经 HTTP 确认端点调用 Resolve 投递用户决定，被等待的 goroutine 据此继续。
//
// 设计要点：
//   - 按 approvalID 维护一个 chan ApprovalDecision（缓冲 1），Await/Resolve 解耦；
//   - 等待带超时，防止用户关闭页面后 goroutine 永久悬挂；
//   - 进程重启时未决等待随 goroutine 退出而失效，不做持久化（写操作尚未发生，无脏状态）。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ApprovalDecision 用户审批结果。
type ApprovalDecision struct {
	Approved bool
}

// approvalEntry 一个等待中的审批。
type approvalEntry struct {
	ch    chan ApprovalDecision
	until time.Time
}

// ApprovalBroker 审批中介。
type ApprovalBroker struct {
	mu      sync.Mutex
	entries map[string]*approvalEntry
}

// defaultApprovalTimeout 等待用户决定的上限（超时按拒绝处理）。
const defaultApprovalTimeout = 10 * time.Minute

// NewApprovalBroker 创建中介。
func NewApprovalBroker() *ApprovalBroker {
	return &ApprovalBroker{entries: map[string]*approvalEntry{}}
}

// NewApproval 生成令牌并登记一个等待通道。返回 approvalID。
func (b *ApprovalBroker) NewApproval() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := newApprovalID()
	b.entries[id] = &approvalEntry{
		ch:    make(chan ApprovalDecision, 1),
		until: time.Now().Add(defaultApprovalTimeout),
	}
	return id
}

// Await 阻塞等待用户决定。ctx 取消或超时时返回 (false,false)（ok 表示是否收到明确决定）。
func (b *ApprovalBroker) Await(ctx context.Context, approvalID string) (decision ApprovalDecision, ok bool) {
	b.mu.Lock()
	entry, exists := b.entries[approvalID]
	b.mu.Unlock()
	if !exists {
		return ApprovalDecision{}, false
	}
	timer := time.NewTimer(time.Until(entry.until))
	defer timer.Stop()

	select {
	case d := <-entry.ch:
		b.remove(approvalID)
		return d, true
	case <-timer.C:
		b.remove(approvalID)
		return ApprovalDecision{Approved: false}, false
	case <-ctx.Done():
		b.remove(approvalID)
		return ApprovalDecision{Approved: false}, false
	}
}

// Resolve 投递用户决定。令牌不存在（已超时/重复提交）时返回 false。
func (b *ApprovalBroker) Resolve(approvalID string, approved bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.entries[approvalID]
	if !ok {
		return false
	}
	select {
	case entry.ch <- ApprovalDecision{Approved: approved}:
		return true
	default:
		return false
	}
}

// newApprovalID 生成随机令牌。
func newApprovalID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return "apr_" + hex.EncodeToString(b[:])
}

func (b *ApprovalBroker) remove(id string) { b.mu.Lock(); defer b.mu.Unlock(); delete(b.entries, id) }
