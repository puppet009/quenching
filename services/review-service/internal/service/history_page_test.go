// Package service 历史分页钳制纯函数单测（T4-16）。
package service

import "testing"

// TestNormalizePage 覆盖页码/页大小的默认值、下限与上限。
func TestNormalizePage(t *testing.T) {
	cases := []struct {
		name         string
		page, size   int
		wantP, wantS int
	}{
		{"正常值原样返回", 2, 30, 2, 30},
		{"首页一页", 1, 20, 1, 20},
		{"page 为 0 用默认 1", 0, 20, 1, 20},
		{"page 为负用默认 1", -5, 20, 1, 20},
		{"size 为 0 用默认 20", 3, 0, 3, historyDefaultSize},
		{"size 为负用默认 20", 3, -1, 3, historyDefaultSize},
		{"size 超过 100 钳到 100", 1, 500, 1, historyMaxSize},
		{"两者同时非法都回默认", 0, 0, historyDefaultPage, historyDefaultSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotP, gotS := NormalizePage(tc.page, tc.size)
			if gotP != tc.wantP || gotS != tc.wantS {
				t.Errorf("NormalizePage(%d,%d) = (%d,%d), want (%d,%d)",
					tc.page, tc.size, gotP, gotS, tc.wantP, tc.wantS)
			}
		})
	}
}

// TestNormalizePage_OffsetFormula 确认钳制后 offset 公式稳定
// （handler/service 统一用 (page-1)*size）。
func TestNormalizePage_OffsetFormula(t *testing.T) {
	page, size := NormalizePage(3, 20)
	if offset := (page - 1) * size; offset != 40 {
		t.Errorf("第 3 页 offset 应为 40，got %d", offset)
	}
}
