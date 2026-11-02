// behavior_signals.go 行为信号的内存汇聚（行为模拟第二批）。
//
// runner 上报的信号先到 sandbox-service 的本地汇聚点：按沙箱会话保留
// 最近 N 条（环形覆盖），供监控部件/排障接口即时读取；第三批在此增加
// 向 training-service 告警端点的转发。
//
// 只存储 runner 真实上报的事件，本地 fake 降级环境没有 runner，缓冲
// 始终为空，绝不填充模拟信号。
package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// behaviorRingCapacity 每个会话环形缓冲容量（超过后最旧事件被覆盖）。
const behaviorRingCapacity = 200

// SignalForwarder 信号转发器（第三批，由 AlertClient 实现）。
type SignalForwarder interface {
	Forward(ctx context.Context, sig BehaviorSignal) error
}

// BehaviorSignalStore 按会话汇聚行为信号的内存存储（线程安全）。
// 进程重启即清空：它是实时观测缓冲，不是持久化监控数据源
// （持久化与告警状态由 training-service 负责）。
type BehaviorSignalStore struct {
	mu        sync.Mutex
	rings     map[int64][]BehaviorSignal
	capacity  int
	forwarder SignalForwarder
}

// NewBehaviorSignalStore 创建信号汇聚存储。
func NewBehaviorSignalStore() *BehaviorSignalStore {
	return &BehaviorSignalStore{
		rings:    map[int64][]BehaviorSignal{},
		capacity: behaviorRingCapacity,
	}
}

// SetForwarder 注入 training-service 转发器（nil 表示不转发）。
func (s *BehaviorSignalStore) SetForwarder(f SignalForwarder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forwarder = f
}

// Add 汇聚一条信号。信号必须携带会话ID，否则丢弃（无法归属的信号不展示）。
// 汇聚成功后异步转发：转发失败只记日志，不影响本地汇聚，也不阻塞调用方。
func (s *BehaviorSignalStore) Add(sig BehaviorSignal) {
	sessionID := sig.SessionID
	if sessionID == 0 {
		sessionID = sig.SandboxID
	}
	if sessionID == 0 {
		return
	}

	s.mu.Lock()
	forwarder := s.forwarder
	ring := s.rings[sessionID]
	ring = append(ring, sig)
	if len(ring) > s.capacity {
		// 环形覆盖：只保留最近 capacity 条。
		ring = ring[len(ring)-s.capacity:]
	}
	s.rings[sessionID] = ring
	s.mu.Unlock()

	if forwarder != nil {
		go func(sig BehaviorSignal) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := forwarder.Forward(ctx, sig); err != nil {
				log.Printf("[behavior] 会话 %d 信号转发 training-service 失败: %v", sessionID, err)
			}
		}(sig)
	}
}

// List 取指定会话最近的信号（按时间先后，返回副本）。
func (s *BehaviorSignalStore) List(sessionID int64) []BehaviorSignal {
	s.mu.Lock()
	defer s.mu.Unlock()

	ring := s.rings[sessionID]
	if len(ring) == 0 {
		return nil
	}
	out := make([]BehaviorSignal, len(ring))
	copy(out, ring)
	return out
}

// Clear 清空指定会话的信号缓冲（沙箱释放时调用）。
func (s *BehaviorSignalStore) Clear(sessionID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rings, sessionID)
}
