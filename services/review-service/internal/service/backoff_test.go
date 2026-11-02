// Package service 退避策略纯函数单测（T4-16）。
package service

import (
	"testing"
	"time"
)

// TestNextBackoff_Linear 线性退避：5s、10s、15s……
func TestNextBackoff_Linear(t *testing.T) {
	base := 5 * time.Second
	cases := []struct {
		retryCount int
		want       time.Duration
	}{
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 15 * time.Second},
		{5, 25 * time.Second},
	}
	for _, tc := range cases {
		if got := nextBackoff(tc.retryCount, base); got != tc.want {
			t.Errorf("nextBackoff(%d) = %v, want %v", tc.retryCount, got, tc.want)
		}
	}
}

// TestNextBackoff_NonPositiveClampedToFirstStep 非正入参按第 1 次重试处理，
// 避免出现 0 或负退避导致失败任务立即热循环重投。
func TestNextBackoff_NonPositiveClampedToFirstStep(t *testing.T) {
	base := 5 * time.Second
	for _, n := range []int{0, -1, -99} {
		if got := nextBackoff(n, base); got != base {
			t.Errorf("nextBackoff(%d) = %v, want %v（非正入参应钳到首档）", n, got, base)
		}
	}
}

// TestNextBackoff_RespectsCustomBase 自定义基数按比例放大。
func TestNextBackoff_RespectsCustomBase(t *testing.T) {
	if got := nextBackoff(3, 2*time.Second); got != 6*time.Second {
		t.Errorf("nextBackoff(3,2s) = %v, want 6s", got)
	}
}
