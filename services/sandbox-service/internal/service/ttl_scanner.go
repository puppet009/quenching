package service

import (
	"context"
	"time"

	"github.com/quenching/go-common/logger"
	"github.com/quenching/sandbox-service/internal/model"
	"go.uber.org/zap"
)

// Clock 可注入的时钟，单测用假时钟控制时间。
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// NewSystemClock 返回真实时钟（生产用）。
func NewSystemClock() Clock { return systemClock{} }

// TTLScanner 周期性扫描并回收过期沙箱。
// 云上 k3s 环境由主程序启动；无集群/单测时不启动即可，回收逻辑在 Reconciler.ReapExpired 单独可测。
type TTLScanner struct {
	rec      *Reconciler
	clock    Clock
	interval time.Duration
}

func NewTTLScanner(rec *Reconciler, clock Clock, interval time.Duration) *TTLScanner {
	if clock == nil {
		clock = systemClock{}
	}
	if interval <= 0 {
		interval = model.ReapScanInterval
	}
	return &TTLScanner{rec: rec, clock: clock, interval: interval}
}

// Run 阻塞运行，直到 ctx 取消。每 interval 执行一轮回收。
func (s *TTLScanner) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 启动后立即跑一轮，服务重启时能尽快回收上次残留的过期沙箱。
	s.scanOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanOnce(ctx)
		}
	}
}

func (s *TTLScanner) scanOnce(ctx context.Context) {
	reaped, err := s.rec.ReapExpired(ctx, s.clock.Now())
	if err != nil {
		logger.L().Warn("TTL 回收扫描失败", zap.Error(err))
		return
	}
	if len(reaped) > 0 {
		logger.L().Info("TTL 回收沙箱", zap.Int64s("session_ids", reaped))
	}
}
