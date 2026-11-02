// 静态内置模板注册表（T3-2 建立，T3-3 校准要点与别名）。
//
// 覆盖 MVP 首批 8 种基础故障（《MVP需求文档》"首批故障类型（8种）"），
// 并维护"故障类型取值 → 规范 Key"的别名归一：
//   - 细粒度 token（慢SQL/slow_sql、CPU/cpu…）直接命中具体模板；
//   - 粗粒度 category（database/cache…，见 fault-engine defects.category 注释）
//     在存在歧义时归一到该域最典型的故障（如 database→慢SQL、cache→缓存雪崩）。
//     T3-3 已结合首批种子缺陷（category=database/network/memory）校准：
//     现有粗粒度映射与种子域方向一致，并补齐同域常见词（postgres/网络丢包/jvm 等）；
//   - 未登记的取值一律回退 generic，不报错。
//
// 要点内容已于 T3-3 按 8 个故障域逐域校准：三段分别给出"观察→定位→假设验证→
// 修复复测"的专家排查框架、必讲核心概念与工具、以及基于用户操作的亮点/典型错误
// 评判点，供方法论复盘 Prompt 注入使用。
package service

import "strings"

// 规范故障类型标识（与注册表 Key 对齐）。
const (
	KeySlowSQL         = "slow_sql"          // 慢SQL
	KeyCPUHigh         = "cpu_high"          // CPU飙高
	KeyDiskIO          = "disk_io"           // 磁盘IO瓶颈
	KeyCacheAvalanche  = "cache_avalanche"   // 缓存雪崩
	KeyServiceTimeout  = "service_timeout"   // 服务超时/熔断
	KeyConnPoolExhaust = "conn_pool_exhaust" // 数据库连接池耗尽
	KeyThreadPoolFull  = "thread_pool_full"  // 线程池打满
	KeyMemoryLeak      = "memory_leak"       // 内存泄漏
)

// builtinTemplates 规范 Key → 模板。
var builtinTemplates = map[string]Template{
	KeySlowSQL: {
		Key:         KeySlowSQL,
		DisplayName: "慢SQL",
		Methodology: "先用业务监控与慢查询日志确认现象、影响面（报错/超时比例）与具体 SQL，再用 EXPLAIN（必要时 EXPLAIN ANALYZE）看执行计划；按 type/key/rows/Extra 区分全表扫描、索引失效（函数/隐式类型转换/最左前缀违背/前导模糊匹配）、统计信息过期、回表过多等成因；定位后对症处理（加合适索引、改写 SQL、更新统计信息），并复测执行计划、执行耗时与业务指标确认闭环，最后沉淀这类语句的通用排查路径",
		Knowledge:   "B+树索引结构与最左匹配原则、回表与覆盖索引、EXPLAIN 关键字段（type/key/key_len/rows/Extra）、优化器统计信息与代价选择、慢查询日志与 long_query_time、隐式类型转换/OR/前导 LIKE 对索引的影响",
		Highlights:  "做得好的：先开慢查询日志定位到具体 SQL 再动手、用 EXPLAIN 证据而非猜测定因、改后复测执行计划与耗时；典型错误：不看执行计划直接重写 SQL、把统计信息过期导致的选错索引误判成索引或 SQL 本身问题、加完索引不验证是否被使用、只盯单条语句不看 QPS 叠加与业务影响面",
	},
	KeyCPUHigh: {
		Key:         KeyCPUHigh,
		DisplayName: "CPU飙高",
		Methodology: "先用 top/vmstat 确认是整机 CPU 高并区分用户态（us）/内核态（sy）/iowait（wa），再 top -Hp 定位高 CPU 线程，把线程 ID 转十六进制后在 jstack 线程栈（或 async-profiler 火焰图）中定位方法；按证据区分计算密集/死循环/锁竞争/频繁 GC（结合 GC 日志与 GC 次数时间），提出假设后用重复采样栈或 Profiling 验证，修复后观察 CPU 与响应时间同时回落",
		Knowledge:   "top/top -Hp/pidstat/vmstat 的判读、用户态与内核态 CPU、线程 ID 到 nid 的十六进制转换、jstack 线程状态、火焰图、GC 原理与频繁 Full GC 的诱因（内存泄漏/元空间/System.gc）、上下文切换",
		Highlights:  "做得好的：按进程→线程→栈逐层收敛、多次采样确认热点、用 GC 日志排除/确认 GC 因素；典型错误：一看到 CPU 高就重启或加机器、只抓一次栈就下结论、停在'GC 压力大'的表层而不追 Full GC 的根因、把 us 高与 wa 高混为一谈导致错误方向",
	},
	KeyDiskIO: {
		Key:         KeyDiskIO,
		DisplayName: "磁盘IO瓶颈",
		Methodology: "先用 iostat -x 确认 iowait、await、%util 与读写类型，iotop/pidstat -d 定位高 IO 进程与文件；区分日志同步刷盘/大文件顺序写/随机读/swap 换入换出等来源，结合 page cache 命中率与磁盘容量分析；调整（异步刷盘/日志级别/落盘策略/扩容）后复查 await、%util 与业务耗时回落",
		Knowledge:   "iowait 的真实含义（CPU 等待 IO 的空闲比例）、await（队列+服务时间）与 %util、IOPS 与吞吐、顺序/随机 IO、page cache 与脏页刷盘、swap 与 swappiness、日志 appender 刷盘策略、iotop",
		Highlights:  "做得好的：先用 await/%util 确认确属 IO 瓶颈而非 CPU/内存、区分读还是写再定位进程；典型错误：只看磁盘空间或使用率不看延迟与队列、笼统归因'磁盘慢'却不查日志写入方式与刷盘策略、忽视 swap 导致的伪 IO 瓶颈",
	},
	KeyCacheAvalanche: {
		Key:         KeyCacheAvalanche,
		DisplayName: "缓存雪崩",
		Methodology: "先确认大批 key 同时失效（TTL 相同/缓存节点宕机）→ 请求穿透到 DB → 下游 CPU 与连接数飙升的完整因果链；核对 TTL 分布、缓存命中率与 DB QPS 拐点的时间对应；评估 TTL 加随机值、热点预热、限流降级、分布式锁/单飞等方案，并审视方案副作用（锁粒度、限流对用户的影响），实施后观察命中率回升与 DB 压力回落",
		Knowledge:   "雪崩（大面积同时失效）/击穿（单个热点 key 失效）/穿透（查不存在的数据）三者区别、TTL 集中失效与随机偏移、缓存命中率、限流降级与熔断、分布式锁粒度与单飞（singleflight）、布隆过滤器、缓存永不过期+异步刷新",
		Highlights:  "做得好的：先看清'失效→穿透→DB 被打穿'整条因果链再选方案、评估方案自身的副作用、用命中率与 DB 指标验证；典型错误：把雪崩/击穿/穿透混用导致方案选错、高置信度地'加分布式锁'却因锁粒度太粗或锁失败无降级引发二次雪崩、只盯 Redis 指标不看被打穿的数据库",
	},
	KeyServiceTimeout: {
		Key:         KeyServiceTimeout,
		DisplayName: "服务超时/熔断",
		Methodology: "先从入口报错沿调用链（Trace/日志时间戳）找到真正变慢的下游服务或依赖（含网络丢包、DB、第三方），区分下游变慢、超时配置不合理、熔断触发与资源（线程/连接）耗尽；核对重试策略是否因无退避或重试放大形成重试风暴，确认熔断是保护结果而非根因；恢复后梳理超时、重试（次数/退避）、熔断阈值与降级的合理边界",
		Knowledge:   "分布式调用链 Trace、超时传递与排队论放大效应、熔断器三态（关闭/打开/半开）、重试风暴与指数退避+抖动、舱壁隔离与限流、网络丢包/RTT 对超时的影响、TCP 重传",
		Highlights:  "做得好的：顺链路定位到真正变慢的下游而不是只看报错入口、把熔断当作线索去追被保护的依赖；典型错误：把熔断当成根因而非保护结果、盲目加大超时或无退避重试导致雪崩放大、只在入口看现象不下钻到下游与网络指标",
	},
	KeyConnPoolExhaust: {
		Key:         KeyConnPoolExhaust,
		DisplayName: "数据库连接池耗尽",
		Methodology: "先确认连接池活跃数/使用率与获取连接等待（等待队列、获取超时异常），再区分三类成因：连接泄漏（借了未在 finally/作用域中归还）、慢 SQL/慢事务长期持有连接、池容量本身过小；用连接来源、持有时间与 DB 侧 processlist/活动事务验证假设；修复后核对连接数回落、等待消失且业务恢复，避免只调大上限",
		Knowledge:   "连接池核心参数（最大连接/最小空闲/获取超时/空闲回收/最大生命周期）、连接泄漏与归还时机、获取连接超时异常、连接与线程的一一对应、DB 最大连接数上限、长事务持有连接、SHOW PROCESSLIST/pg_stat_activity",
		Highlights:  "做得好的：先区分泄漏、慢占用与容量三类成因再动手、查连接持有来源与未释放代码路径；典型错误：直接调大最大连接数掩盖泄漏（只推迟爆发并可能压垮 DB）、不核对代码是否在异常路径也正确归还连接、忽视慢事务才是连接被长期占有的根因",
	},
	KeyThreadPoolFull: {
		Key:         KeyThreadPoolFull,
		DisplayName: "线程池打满",
		Methodology: "先看线程池活跃线程数、队列堆积长度与拒绝异常，再用线程栈定位线程被什么任务占住（慢下游 RPC/阻塞 IO/锁等待/任务量激增）；对照核心/最大线程、队列容量与拒绝策略判断是容量问题还是阻塞问题；通过消除阻塞、隔离线程池、合理扩容或限流解决，实施后观察队列消化与拒绝消失",
		Knowledge:   "ThreadPoolExecutor 核心/最大线程、工作队列与拒绝策略（Abort/CallerRuns/Discard）、线程生命周期与回收、线程阻塞与上下文切换、舱壁线程隔离、慢下游占满线程的传播、Tomcat/业务线程池区别",
		Highlights:  "做得好的：先看队列与任务类型、用线程栈定位占用来源、区分阻塞与容量；典型错误：只调大线程数不解决阻塞来源（反而增加下游压力与上下文切换）、忽视慢下游同步调用占住全部线程的根因、不看拒绝策略导致故障以异常或调用方被拖死等不同形式爆发",
	},
	KeyMemoryLeak: {
		Key:         KeyMemoryLeak,
		DisplayName: "内存泄漏",
		Methodology: "先观察堆内存使用趋势与 GC 频率/耗时（GC 后老年代是否回不去、Full GC 是否频繁），区分内存泄漏、内存配置不足与瞬时峰值；在内存高位抓堆 dump，用 MAT/jvisualvm 分析 Dominator Tree 与 GC Root 引用链定位泄漏对象及其持有方（静态集合/缓存/ThreadLocal/未关闭资源）；修复后持续观察多轮 GC 后堆稳定回落",
		Knowledge:   "JVM 堆分代与元空间、GC 与 Full GC、jstat/jmap/GC 日志、堆 dump 与 MAT（Dominator Tree/Histogram/Leak Suspects）、GC Roots 与引用链、强/软/弱/虚引用、ThreadLocal 未 remove、无界缓存/静态集合泄漏",
		Highlights:  "做得好的：用堆 dump 和 GC Root 引用链定位到具体泄漏对象与持有者、修复后跨多轮 GC 持续观察；典型错误：只调大堆内存（推迟 OOM 不解决泄漏）、看到 OOM 就认定泄漏而不排除配置不足或大对象、凭现象猜对象不抓 dump 取证",
	},
	GenericKey: {
		Key:         GenericKey,
		DisplayName: "通用故障",
		Methodology: "先确认现象与影响面（什么时间、什么指标、影响哪些用户/接口），再用监控与日志按时间线定位异常组件；基于证据提出可证伪的假设并逐一验证，收敛根因后修复，最后复测并观察指标回归、沉淀可复用的排查步骤",
		Knowledge:   "该类故障涉及的核心概念、关键排查命令与工具的用途及判读方法、该类故障的常见成因与区分手段",
		Highlights:  "做得好的：按证据逐层收敛、修复后复测并观察、能区分现象与根因；典型错误：凭直觉下结论、把保护机制或报错表象当成根因、不验证修复效果就关闭问题",
	},
}

// faultTypeAliases 别名（归一化后的小写无分隔 token）→ 规范 Key。
//
// 归一规则见 normalizeKey：去除空格、下划线、连字符、斜杠并转小写，
// 因此注册时键也使用无分隔形式。粗粒度 category 的映射 T3-3 已按首批种子
// 缺陷词表校准（database/network/memory 三域方向一致），并补齐同域常见词。
var faultTypeAliases = map[string]string{
	// 慢SQL
	"慢sql":       KeySlowSQL,
	"慢查询":        KeySlowSQL,
	"slowsql":    KeySlowSQL,
	"slowquery":  KeySlowSQL,
	"database":   KeySlowSQL, // 粗粒度：database 域含慢SQL/连接池耗尽，默认典型代表（种子 slow-query-order-01）
	"db":         KeySlowSQL,
	"mysql":      KeySlowSQL,
	"postgres":   KeySlowSQL,
	"postgresql": KeySlowSQL,
	"pgsql":      KeySlowSQL,
	"sql":        KeySlowSQL,
	"数据库":        KeySlowSQL,
	"索引失效":       KeySlowSQL,
	"全表扫描":       KeySlowSQL,

	// CPU飙高
	"cpu":      KeyCPUHigh,
	"cpuhigh":  KeyCPUHigh,
	"cpuspike": KeyCPUHigh,
	"cpuload":  KeyCPUHigh,
	"cpu飙高":    KeyCPUHigh,
	"cpu100":   KeyCPUHigh,
	"高cpu":     KeyCPUHigh,

	// 磁盘IO瓶颈
	"diskio": KeyDiskIO,
	"disk":   KeyDiskIO,
	"iowait": KeyDiskIO,
	"磁盘io":   KeyDiskIO,
	"磁盘io瓶颈": KeyDiskIO,
	"磁盘":     KeyDiskIO,
	"io瓶颈":   KeyDiskIO,
	"磁盘繁忙":   KeyDiskIO,

	// 缓存雪崩
	"cacheavalanche": KeyCacheAvalanche,
	"cache":          KeyCacheAvalanche, // 粗粒度：缓存域默认雪崩（最典型大面积故障）
	"redis":          KeyCacheAvalanche,
	"缓存雪崩":           KeyCacheAvalanche,
	"雪崩":             KeyCacheAvalanche,
	"缓存击穿":           KeyCacheAvalanche, // 同域排查框架（命中率/穿透/DB压力）一致
	"缓存穿透":           KeyCacheAvalanche,

	// 服务超时/熔断
	"servicetimeout": KeyServiceTimeout,
	"timeout":        KeyServiceTimeout,
	"circuitbreaker": KeyServiceTimeout,
	"network":        KeyServiceTimeout, // 粗粒度：网络域（种子 net-loss-product-01）默认超时/链路类
	"net":            KeyServiceTimeout,
	"网络":             KeyServiceTimeout,
	"网络丢包":           KeyServiceTimeout,
	"丢包":             KeyServiceTimeout,
	"rpc":            KeyServiceTimeout,
	"服务超时":           KeyServiceTimeout,
	"熔断":             KeyServiceTimeout,
	"超时熔断":           KeyServiceTimeout,
	"微服务":            KeyServiceTimeout,

	// 数据库连接池耗尽
	"connpoolexhaust": KeyConnPoolExhaust,
	"connectionpool":  KeyConnPoolExhaust,
	"connpool":        KeyConnPoolExhaust,
	"连接池":             KeyConnPoolExhaust,
	"连接池耗尽":           KeyConnPoolExhaust,
	"数据库连接池":          KeyConnPoolExhaust,
	"连接泄漏":            KeyConnPoolExhaust,

	// 线程池打满
	"threadpoolfull": KeyThreadPoolFull,
	"threadpool":     KeyThreadPoolFull,
	"threads":        KeyThreadPoolFull,
	"线程池":            KeyThreadPoolFull,
	"线程池打满":          KeyThreadPoolFull,
	"线程打满":           KeyThreadPoolFull,

	// 内存泄漏
	"memoryleak": KeyMemoryLeak,
	"oom":        KeyMemoryLeak,
	"jvm":        KeyMemoryLeak, // 粗粒度：JVM 域（种子 mem-leak-user-01）默认内存泄漏
	"java":       KeyMemoryLeak,
	"memory":     KeyMemoryLeak, // 粗粒度：内存域默认泄漏
	"内存泄漏":       KeyMemoryLeak,
	"内存溢出":       KeyMemoryLeak,
	"内存":         KeyMemoryLeak,
}

// StaticProvider 静态内置模板供应器，零依赖、可直接在装配层使用。
type StaticProvider struct{}

// NewStaticProvider 创建静态模板供应器。
func NewStaticProvider() StaticProvider {
	return StaticProvider{}
}

// 编译期断言：StaticProvider 实现 Provider。
var _ Provider = StaticProvider{}

// TemplateByFaultType 按故障类型返回模板；空/未知类型回退 generic。
func (StaticProvider) TemplateByFaultType(faultType string) Template {
	if key, ok := faultTypeAliases[normalizeKey(faultType)]; ok {
		if t, ok := builtinTemplates[key]; ok {
			return t
		}
	}
	return builtinTemplates[GenericKey]
}

// normalizeKey 把故障类型取值归一为无分隔小写 token 用于别名查表。
// 同时兼容中文（空格/斜杠/下划线/连字符剔除即可，中文不受 ToLower 影响）。
func normalizeKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, sep := range []string{" ", "_", "-", "/", "\\"} {
		s = strings.ReplaceAll(s, sep, "")
	}
	return s
}
