package service

import (
	"strings"
	"testing"
)

func TestStaticProvider_EightFaultsHit(t *testing.T) {
	p := NewStaticProvider()

	// 8 个规范 key 直接命中，且模板字段完整（generic 之外不得有空要点）。
	keys := []string{
		KeySlowSQL, KeyCPUHigh, KeyDiskIO, KeyCacheAvalanche,
		KeyServiceTimeout, KeyConnPoolExhaust, KeyThreadPoolFull, KeyMemoryLeak,
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			tpl := p.TemplateByFaultType(key)
			if tpl.Key != key {
				t.Errorf("faultType %q → Key = %q, want %q", key, tpl.Key, key)
			}
			if tpl.IsGeneric() {
				t.Errorf("faultType %q 不应命中 generic", key)
			}
			if tpl.DisplayName == "" || tpl.Methodology == "" || tpl.Knowledge == "" || tpl.Highlights == "" {
				t.Errorf("模板 %s 存在空字段: %+v", key, tpl)
			}
		})
	}
}

func TestStaticProvider_Aliases(t *testing.T) {
	p := NewStaticProvider()
	cases := []struct {
		name      string
		faultType string
		wantKey   string
	}{
		// 英文别名 / 分隔符归一
		{"慢SQL下划线", "slow_sql", KeySlowSQL},
		{"慢SQL连字符", "slow-sql", KeySlowSQL},
		{"慢SQL空格大写", " Slow SQL ", KeySlowSQL},
		{"mysql", "MySQL", KeySlowSQL},
		{"cpu 带空格", "cpu high", KeyCPUHigh},
		{"磁盘斜杠", "disk/io", KeyDiskIO},
		// 中文别名
		{"中文慢SQL", "慢SQL", KeySlowSQL},
		{"中文慢查询", "慢查询", KeySlowSQL},
		{"中文CPU飙高", "CPU飙高", KeyCPUHigh},
		{"中文磁盘IO瓶颈", "磁盘IO瓶颈", KeyDiskIO},
		{"中文缓存雪崩", "缓存雪崩", KeyCacheAvalanche},
		{"中文熔断", "熔断", KeyServiceTimeout},
		{"中文连接池耗尽", "连接池耗尽", KeyConnPoolExhaust},
		{"中文线程池打满", "线程池打满", KeyThreadPoolFull},
		{"中文内存泄漏", "内存泄漏", KeyMemoryLeak},
		// 粗粒度 category 别名（T3-3 已结合首批种子缺陷校准）
		{"database 粗粒度", "database", KeySlowSQL},
		{"db 粗粒度", "db", KeySlowSQL},
		{"postgres 粗粒度", "Postgres", KeySlowSQL},
		{"cache 粗粒度", "cache", KeyCacheAvalanche},
		{"redis 粗粒度", "redis", KeyCacheAvalanche},
		{"network 粗粒度", "network", KeyServiceTimeout},
		{"网络丢包粗粒度", "网络丢包", KeyServiceTimeout},
		{"jvm 粗粒度", "jvm", KeyMemoryLeak},
		{"内存粗粒度", "内存", KeyMemoryLeak},
		// T3-3 补齐的同域常见词
		{"连接泄漏", "连接泄漏", KeyConnPoolExhaust},
		{"缓存穿透", "缓存穿透", KeyCacheAvalanche},
		{"线程打满", "线程打满", KeyThreadPoolFull},
		{"内存溢出", "内存溢出", KeyMemoryLeak},
		{"全表扫描", "全表扫描", KeySlowSQL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.TemplateByFaultType(tc.faultType).Key; got != tc.wantKey {
				t.Errorf("TemplateByFaultType(%q) = %q, want %q", tc.faultType, got, tc.wantKey)
			}
		})
	}
}

func TestStaticProvider_GenericFallback(t *testing.T) {
	p := NewStaticProvider()

	for _, faultType := range []string{"", "   ", "unknown_fault", "量子涨落", "null"} {
		tpl := p.TemplateByFaultType(faultType)
		if !tpl.IsGeneric() {
			t.Errorf("faultType %q 应回退 generic，实际 %q", faultType, tpl.Key)
		}
		if tpl.Key != GenericKey || tpl.Methodology == "" || tpl.Knowledge == "" || tpl.Highlights == "" {
			t.Errorf("generic 兜底模板不完整: %+v", tpl)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"slow_sql", "slowsql"},
		{"Slow-SQL", "slowsql"},
		{"  CPU HIGH ", "cpuhigh"},
		{"disk/io", "diskio"},
		{`disk\io`, "diskio"},
		{"缓存雪崩", "缓存雪崩"}, // 中文不受影响
	}
	for _, tc := range cases {
		if got := normalizeKey(tc.in); got != tc.want {
			t.Errorf("normalizeKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTemplate_PromptHints(t *testing.T) {
	tpl := builtinTemplates[KeySlowSQL]
	hints := tpl.PromptHints()

	for _, want := range []string{
		"【按故障类型的复盘关注点】",
		"方法论提炼参考框架：",
		"知识点梳理关注点：",
		"亮点坑点评判关注点：",
	} {
		if !strings.Contains(hints, want) {
			t.Errorf("PromptHints 缺少固定片段 %q:\n%s", want, hints)
		}
	}
	if !strings.Contains(hints, tpl.Methodology) || !strings.Contains(hints, tpl.Knowledge) || !strings.Contains(hints, tpl.Highlights) {
		t.Errorf("PromptHints 未完整渲染三类要点:\n%s", hints)
	}

	// generic 模板同样可渲染，调用方无需区分。
	if !strings.Contains(builtinTemplates[GenericKey].PromptHints(), "方法论提炼参考框架：") {
		t.Error("generic 模板 PromptHints 不可用")
	}
}

// TestStaticProvider_EightFaultsDomainAnchors 校验 T3-3 校准后的 8 条模板
// 不是互相雷同的占位框架：每条都必须含该故障域专属的锚点工具/概念，
// 且三段都保留"做得好的/典型错误"评判结构，保证逐故障抽检复盘质量。
func TestStaticProvider_EightFaultsDomainAnchors(t *testing.T) {
	cases := map[string][]string{
		KeySlowSQL:         {"EXPLAIN", "索引", "慢查询"},
		KeyCPUHigh:         {"jstack", "火焰图", "GC"},
		KeyDiskIO:          {"iostat", "await", "%util"},
		KeyCacheAvalanche:  {"雪崩", "击穿", "穿透"},
		KeyServiceTimeout:  {"调用链", "重试风暴", "熔断"},
		KeyConnPoolExhaust: {"连接泄漏", "获取连接超时", "连接池"},
		KeyThreadPoolFull:  {"拒绝策略", "队列", "线程池"},
		KeyMemoryLeak:      {"堆 dump", "GC Root", "MAT"},
	}
	for key, anchors := range cases {
		t.Run(key, func(t *testing.T) {
			tpl := builtinTemplates[key]
			all := tpl.Methodology + tpl.Knowledge + tpl.Highlights
			for _, anchor := range anchors {
				if !strings.Contains(all, anchor) {
					t.Errorf("模板 %s 缺少故障域锚点 %q", key, anchor)
				}
			}
			if !strings.Contains(tpl.Highlights, "做得好的") || !strings.Contains(tpl.Highlights, "典型错误") {
				t.Errorf("模板 %s 亮点坑点缺少 做得好的/典型错误 结构", key)
			}
			// 校准后要点应足够具体（超过占位框架的信息量下限）。
			if len([]rune(tpl.Methodology)) < 50 || len([]rune(tpl.Knowledge)) < 20 {
				t.Errorf("模板 %s 要点过短，疑似仍为占位内容", key)
			}
		})
	}
}

// TestStaticProvider_TemplatesDistinct 防止 8 条模板要点互相雷同（占位框架的典型问题）。
func TestStaticProvider_TemplatesDistinct(t *testing.T) {
	keys := []string{
		KeySlowSQL, KeyCPUHigh, KeyDiskIO, KeyCacheAvalanche,
		KeyServiceTimeout, KeyConnPoolExhaust, KeyThreadPoolFull, KeyMemoryLeak,
	}
	seen := map[string]string{}
	for _, key := range keys {
		m := builtinTemplates[key].Methodology
		if prev, dup := seen[m]; dup {
			t.Errorf("模板 %s 与 %s 的 Methodology 完全相同，模板未差异化", key, prev)
		}
		seen[m] = key
	}
}
