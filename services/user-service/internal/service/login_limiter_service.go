// 登录限流与异常检测。
//
// 保护机制：
//   - IP 维度：同一 IP 短时间内登录失败次数过多 → 限流
//   - 账号维度：同一账号连续失败 → 临时锁定
//   - 异常告警：检测到暴力破解行为时触发告警回调
package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// LoginAlertLevel 告警级别。
type LoginAlertLevel string

const (
	LoginAlertWarn  LoginAlertLevel = "warn"  // 警告：多次失败
	LoginAlertError LoginAlertLevel = "error" // 严重：疑似暴力破解
)

// LoginAlertEvent 告警事件。
type LoginAlertEvent struct {
	Level     LoginAlertLevel `json:"level"`
	Type      string          `json:"type"`       // ip_bruteforce / account_lockout
	Target    string          `json:"target"`     // IP 或 账号
	FailCount int             `json:"fail_count"` // 失败次数
	Window    string          `json:"window"`     // 时间窗口
	Message   string          `json:"message"`
	Timestamp time.Time       `json:"timestamp"`
}

// LoginAlertCallback 告警回调函数（生产环境接入告警系统）。
type LoginAlertCallback func(ctx context.Context, event LoginAlertEvent)

// LoginLimiter 登录限流器。
//
// 双维度限流：
//   - IP 限流：防止暴力破解
//   - 账号限流：防止被撞库
type LoginLimiter struct {
	mu sync.Mutex

	// IP 维度失败记录
	ipFailures map[string]*loginFailureRecord
	// 账号维度失败记录
	accountFailures map[string]*loginFailureRecord

	// 配置
	ipMaxFails     int           // IP 最大失败次数
	ipWindow       time.Duration // IP 时间窗口
	ipLockDuration time.Duration // IP 锁定时长

	accountMaxFails     int           // 账号最大失败次数
	accountWindow       time.Duration // 账号时间窗口
	accountLockDuration time.Duration // 账号锁定时长

	alertCb LoginAlertCallback // 告警回调（可为 nil）
}

type loginFailureRecord struct {
	failTimes     []time.Time // 失败时间点
	locked        bool
	lockedAt      time.Time
	lockedTill    time.Time
	alertSent     bool // 是否已发送过锁定（error）告警
	warnAlertSent bool // 是否已发送过半阈值（warn）告警
}

// LoginLimiterConfig 限流器配置。
type LoginLimiterConfig struct {
	IPMaxFails     int           // IP 最大失败次数（默认 10）
	IPWindow       time.Duration // IP 时间窗口（默认 10 分钟）
	IPLockDuration time.Duration // IP 锁定时长（默认 30 分钟）

	AccountMaxFails     int           // 账号最大失败次数（默认 5）
	AccountWindow       time.Duration // 账号时间窗口（默认 15 分钟）
	AccountLockDuration time.Duration // 账号锁定时长（默认 30 分钟）
}

// DefaultLoginLimiterConfig 默认配置。
func DefaultLoginLimiterConfig() LoginLimiterConfig {
	return LoginLimiterConfig{
		IPMaxFails:          10,
		IPWindow:            10 * time.Minute,
		IPLockDuration:      30 * time.Minute,
		AccountMaxFails:     5,
		AccountWindow:       15 * time.Minute,
		AccountLockDuration: 30 * time.Minute,
	}
}

// NewLoginLimiter 创建登录限流器。
func NewLoginLimiter(cfg LoginLimiterConfig) *LoginLimiter {
	if cfg.IPMaxFails <= 0 {
		cfg.IPMaxFails = 10
	}
	if cfg.IPWindow <= 0 {
		cfg.IPWindow = 10 * time.Minute
	}
	if cfg.IPLockDuration <= 0 {
		cfg.IPLockDuration = 30 * time.Minute
	}
	if cfg.AccountMaxFails <= 0 {
		cfg.AccountMaxFails = 5
	}
	if cfg.AccountWindow <= 0 {
		cfg.AccountWindow = 15 * time.Minute
	}
	if cfg.AccountLockDuration <= 0 {
		cfg.AccountLockDuration = 30 * time.Minute
	}

	return &LoginLimiter{
		ipFailures:          make(map[string]*loginFailureRecord),
		accountFailures:     make(map[string]*loginFailureRecord),
		ipMaxFails:          cfg.IPMaxFails,
		ipWindow:            cfg.IPWindow,
		ipLockDuration:      cfg.IPLockDuration,
		accountMaxFails:     cfg.AccountMaxFails,
		accountWindow:       cfg.AccountWindow,
		accountLockDuration: cfg.AccountLockDuration,
	}
}

// SetAlertCallback 设置告警回调。
func (l *LoginLimiter) SetAlertCallback(cb LoginAlertCallback) {
	l.alertCb = cb
}

// CheckLoginAllowed 检查是否允许登录尝试。
// 返回是否允许 + 剩余尝试次数。
func (l *LoginLimiter) CheckLoginAllowed(_ context.Context, ip, account string) (bool, int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查 IP 是否被锁定
	ipRec := l.ipFailures[ip]
	if ipRec != nil && ipRec.locked {
		if time.Now().Before(ipRec.lockedTill) {
			return false, 0, fmt.Errorf("该IP登录失败次数过多，请 %s 后再试",
				formatDuration(time.Until(ipRec.lockedTill)))
		}
		// 锁定已过期，清理
		delete(l.ipFailures, ip)
	}

	// 检查账号是否被锁定
	accRec := l.accountFailures[account]
	if accRec != nil && accRec.locked {
		if time.Now().Before(accRec.lockedTill) {
			return false, 0, fmt.Errorf("账号已被临时锁定，请 %s 后再试",
				formatDuration(time.Until(accRec.lockedTill)))
		}
		delete(l.accountFailures, account)
	}

	// 计算剩余尝试次数（取 IP 和账号中较小的）
	ipRemaining := l.ipMaxFails - l.countRecentFails(ipRec, l.ipWindow)
	accRemaining := l.accountMaxFails - l.countRecentFails(accRec, l.accountWindow)

	remaining := ipRemaining
	if accRemaining < remaining {
		remaining = accRemaining
	}
	if remaining < 0 {
		remaining = 0
	}

	return true, remaining, nil
}

// RecordLoginFailure 记录一次登录失败。
// 返回是否触发了锁定。
func (l *LoginLimiter) RecordLoginFailure(ctx context.Context, ip, account string) (locked bool, lockDuration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// IP 维度
	ipRec := l.getOrCreateIPRecord(ip)
	ipRec.failTimes = append(ipRec.failTimes, now)
	l.cleanOldFailures(ipRec, l.ipWindow)

	ipFailCount := len(ipRec.failTimes)
	if ipFailCount >= l.ipMaxFails && !ipRec.locked {
		ipRec.locked = true
		ipRec.lockedAt = now
		ipRec.lockedTill = now.Add(l.ipLockDuration)
		locked = true
		lockDuration = l.ipLockDuration

		// 触发告警（同步调用；回调实现须保证非阻塞，需要异步时由回调内部自行处理）
		if l.alertCb != nil && !ipRec.alertSent {
			ipRec.alertSent = true
			l.alertCb(ctx, LoginAlertEvent{
				Level:     LoginAlertError,
				Type:      "ip_bruteforce",
				Target:    ip,
				FailCount: ipFailCount,
				Window:    l.ipWindow.String(),
				Message:   fmt.Sprintf("IP %s 在 %s 内登录失败 %d 次，已锁定 %s", ip, l.ipWindow, ipFailCount, l.ipLockDuration),
				Timestamp: now,
			})
		}
	} else if ipFailCount*2 >= l.ipMaxFails && ipFailCount < l.ipMaxFails &&
		!ipRec.warnAlertSent && !ipRec.alertSent && l.alertCb != nil {
		// 严格过半但未锁定时发一次警告；warn/error 标志分离，避免抑制锁定告警
		ipRec.warnAlertSent = true
		l.alertCb(ctx, LoginAlertEvent{
			Level:     LoginAlertWarn,
			Type:      "ip_bruteforce",
			Target:    ip,
			FailCount: ipFailCount,
			Window:    l.ipWindow.String(),
			Message:   fmt.Sprintf("IP %s 在 %s 内登录失败 %d 次，请注意", ip, l.ipWindow, ipFailCount),
			Timestamp: now,
		})
	}

	// 账号维度
	accRec := l.getOrCreateAccountRecord(account)
	accRec.failTimes = append(accRec.failTimes, now)
	l.cleanOldFailures(accRec, l.accountWindow)

	accFailCount := len(accRec.failTimes)
	if accFailCount >= l.accountMaxFails && !accRec.locked {
		accRec.locked = true
		accRec.lockedAt = now
		accRec.lockedTill = now.Add(l.accountLockDuration)
		locked = true
		if lockDuration == 0 || l.accountLockDuration > lockDuration {
			lockDuration = l.accountLockDuration
		}

		if l.alertCb != nil && !accRec.alertSent {
			accRec.alertSent = true
			l.alertCb(ctx, LoginAlertEvent{
				Level:     LoginAlertError,
				Type:      "account_lockout",
				Target:    account,
				FailCount: accFailCount,
				Window:    l.accountWindow.String(),
				Message:   fmt.Sprintf("账号 %s 在 %s 内登录失败 %d 次，已锁定 %s", account, l.accountWindow, accFailCount, l.accountLockDuration),
				Timestamp: now,
			})
		}
	}

	return locked, lockDuration
}

// RecordLoginSuccess 记录登录成功（清除失败记录）。
func (l *LoginLimiter) RecordLoginSuccess(_ context.Context, ip, account string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.ipFailures, ip)
	delete(l.accountFailures, account)
}

// ——— 内部方法 ———

func (l *LoginLimiter) getOrCreateIPRecord(ip string) *loginFailureRecord {
	rec, ok := l.ipFailures[ip]
	if !ok {
		rec = &loginFailureRecord{}
		l.ipFailures[ip] = rec
	}
	return rec
}

func (l *LoginLimiter) getOrCreateAccountRecord(account string) *loginFailureRecord {
	rec, ok := l.accountFailures[account]
	if !ok {
		rec = &loginFailureRecord{}
		l.accountFailures[account] = rec
	}
	return rec
}

func (l *LoginLimiter) countRecentFails(rec *loginFailureRecord, window time.Duration) int {
	if rec == nil {
		return 0
	}
	l.cleanOldFailures(rec, window)
	return len(rec.failTimes)
}

func (l *LoginLimiter) cleanOldFailures(rec *loginFailureRecord, window time.Duration) {
	cutoff := time.Now().Add(-window)
	i := 0
	for _, t := range rec.failTimes {
		if t.After(cutoff) {
			rec.failTimes[i] = t
			i++
		}
	}
	rec.failTimes = rec.failTimes[:i]
}

// formatDuration 格式化剩余时间（用户友好）。
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0 秒"
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if mins > 0 {
		if secs > 0 {
			return fmt.Sprintf("%d 分 %d 秒", mins, secs)
		}
		return fmt.Sprintf("%d 分钟", mins)
	}
	return fmt.Sprintf("%d 秒", secs)
}
