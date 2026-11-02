// Package config 网关配置。
// MVP 阶段从环境变量读取，不引入额外配置文件。
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 网关配置。
type Config struct {
	Addr             string      // 监听地址
	JWTSecret        string      // JWT 密钥
	CORS             CORSConfig  // CORS 配置
	RateLimit        RateConfig  // 限流配置
	Routes           []RouteRule // 路由规则
	WSOrigins        []string    // WebSocket 允许的 Origin（WS_ALLOW_ORIGINS，* 表示全部）
	InternalToken    string      // 领域统计鉴权 INTERNAL_TOKEN
	WSInternalAPIKey string      // 内部事件发布接口的 X-Internal-API-Key（空表示不校验，仅本地开发）
	LogLevel         string      // 日志级别（LOG_LEVEL，默认 info）
	LogDir           string      // 日志目录（LOG_DIR，空表示仅 stdout）
}

// CORSConfig 跨域配置。
type CORSConfig struct {
	Enabled          bool
	AllowOrigins     []string // 允许的源，* 表示全部
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int // 预检请求缓存秒数
}

// RateConfig 限流配置。
type RateConfig struct {
	Enabled      bool
	RatePerSec   int      // 每秒令牌数（填充速率）
	Burst        int      // 令牌桶容量（最大突发）
	KeyByUser    bool     // 按用户ID限流（优先）
	KeyByIP      bool     // 按IP限流（兜底）
	WhitelistIPs []string // 限流白名单（内网IP等）
}

// RouteRule 路由转发规则。
type RouteRule struct {
	PathPrefix string // 路径前缀，如 /api/v1/users
	TargetURL  string // 目标服务地址，如 http://user-service:8080
	Strip      bool   // 是否剥离前缀（默认保留）
	Auth       bool   // 是否需要鉴权（默认需要）
}

// Load 从环境变量加载配置，缺省用默认值。
func Load() *Config {
	return &Config{
		Addr:             getEnv("GATEWAY_ADDR", ":8080"),
		JWTSecret:        getEnv("JWT_SECRET", "quenching-dev-secret"),
		CORS:             loadCORS(),
		RateLimit:        loadRateLimit(),
		Routes:           loadRoutes(),
		WSOrigins:        getEnvSlice("WS_ALLOW_ORIGINS", []string{"*"}),
		InternalToken:    getEnv("INTERNAL_TOKEN", ""),
		WSInternalAPIKey: getEnv("WS_INTERNAL_API_KEY", ""),
		LogLevel:         getEnv("LOG_LEVEL", "info"),
		LogDir:           getEnv("LOG_DIR", ""),
	}
}

func loadCORS() CORSConfig {
	origins := getEnvSlice("CORS_ALLOW_ORIGINS", []string{"*"})
	return CORSConfig{
		Enabled:          getEnvBool("CORS_ENABLED", true),
		AllowOrigins:     origins,
		AllowMethods:     getEnvSlice("CORS_ALLOW_METHODS", []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}),
		AllowHeaders:     getEnvSlice("CORS_ALLOW_HEADERS", []string{"Authorization", "Content-Type", "X-Requested-With", "X-Trace-Id"}),
		ExposeHeaders:    getEnvSlice("CORS_EXPOSE_HEADERS", []string{"X-Trace-Id"}),
		AllowCredentials: getEnvBool("CORS_ALLOW_CREDENTIALS", false),
		MaxAge:           getEnvInt("CORS_MAX_AGE", 86400),
	}
}

func loadRateLimit() RateConfig {
	return RateConfig{
		Enabled:      getEnvBool("RATE_LIMIT_ENABLED", true),
		RatePerSec:   getEnvInt("RATE_LIMIT_RATE", 10),
		Burst:        getEnvInt("RATE_LIMIT_BURST", 30),
		KeyByUser:    getEnvBool("RATE_LIMIT_BY_USER", true),
		KeyByIP:      getEnvBool("RATE_LIMIT_BY_IP", true),
		WhitelistIPs: getEnvSlice("RATE_LIMIT_WHITELIST", nil),
	}
}

// loadRoutes 从环境变量加载路由规则。
// 格式：ROUTE_0=/api/v1/users|http://user-service:8080|true
//
//	路径前缀 | 目标地址 | 是否需要鉴权（可选，默认true）
//
// MVP 阶段硬编码默认路由，环境变量可覆盖。
func loadRoutes() []RouteRule {
	// 默认路由表（本地进程级联调：go run 各服务直连本机统一端口）。
	// 云上部署时用 ROUTE_N 环境变量整表覆盖为 k8s Service DNS，见 .env.example。
	// 匹配采用最长前缀，前缀重叠时更具体的规则优先生效（如 /api/v1/admin/pitfalls
	// 优先于 /api/v1/admin）。
	defaults := []RouteRule{
		// user-service:8085（注册/登录等公开接口不鉴权；/users/me 鉴权）
		{PathPrefix: "/api/v1/auth", TargetURL: "http://localhost:8085", Strip: false, Auth: false},
		{PathPrefix: "/api/v1/users", TargetURL: "http://localhost:8085", Strip: false, Auth: true},
		// user-service:8085（管理后台角色校验：/api/v1/admin/profile）
		{PathPrefix: "/api/v1/admin", TargetURL: "http://localhost:8085", Strip: false, Auth: true},
		// content-service:8082
		{PathPrefix: "/api/v1/projects", TargetURL: "http://localhost:8082", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/project-services", TargetURL: "http://localhost:8082", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/templates", TargetURL: "http://localhost:8082", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/template-media", TargetURL: "http://localhost:8082", Strip: false, Auth: false},
		{PathPrefix: "/api/v1/admin/projects", TargetURL: "http://localhost:8082", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/admin/feedback", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/feedback", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		// sandbox-service:8089（用户面 + 管理面）
		{PathPrefix: "/api/v1/sandboxes", TargetURL: "http://localhost:8089", Strip: false, Auth: true},
		{PathPrefix: "/api/admin/v1/resources", TargetURL: "http://localhost:8089", Strip: false, Auth: true},
		{PathPrefix: "/api/admin/v1/sandboxes", TargetURL: "http://localhost:8089", Strip: false, Auth: true},
		{PathPrefix: "/api/admin/v1/anomalies", TargetURL: "http://localhost:8089", Strip: false, Auth: true},
		// training-service:8086（对话编排 + 评分 + 云原生可观测共 12 组前缀）
		{PathPrefix: "/api/v1/ai", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/sessions", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/training", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/admin/pitfalls", TargetURL: "http://localhost:8082", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/scoring", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/difficulties", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/alerts", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/metrics", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/logs", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/logviewer", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/health", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/ab-stats", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/recovery", TargetURL: "http://localhost:8086", Strip: false, Auth: true},
		// content-service:8082（故障配置唯一属主）
		{PathPrefix: "/api/admin/v1/defects", TargetURL: "http://localhost:8082", Strip: false, Auth: true},
		// review-service:8087（复盘 + 埋点分析：事件/漏斗/看板）
		{PathPrefix: "/api/v1/reviews", TargetURL: "http://localhost:8087", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/events", TargetURL: "http://localhost:8087", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/funnels", TargetURL: "http://localhost:8087", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/dashboard", TargetURL: "http://localhost:8087", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/ai-behavior", TargetURL: "http://localhost:8087", Strip: false, Auth: true},
		{PathPrefix: "/api/v1/ai-board", TargetURL: "http://localhost:8087", Strip: false, Auth: true},
		// 注：/ws（WebSocket 连接）与 /api/v1/events/publish|stats（内部事件发布）
		// 已并入本进程，不走代理，由 main 中 hub + handler 直接处理；
		// 其余 /api/v1/events* 走上面的 review-service 代理。
	}

	// 从 ROUTE_N 环境变量覆盖（任一条目存在即整表替换默认表，编号须连续从 0 开始）
	var custom []RouteRule
	for i := 0; i < 64; i++ {
		key := "ROUTE_" + strconv.Itoa(i)
		val := os.Getenv(key)
		if val == "" {
			break
		}
		parts := strings.Split(val, "|")
		if len(parts) < 2 {
			continue
		}
		rule := RouteRule{
			PathPrefix: strings.TrimSpace(parts[0]),
			TargetURL:  strings.TrimSpace(parts[1]),
			Strip:      false,
			Auth:       true,
		}
		if len(parts) >= 3 {
			rule.Auth = strings.TrimSpace(parts[2]) != "false"
		}
		custom = append(custom, rule)
	}
	if len(custom) > 0 {
		return custom
	}
	return defaults
}

// ---------- 工具函数 ----------

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}

func getEnvSlice(key string, def []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return def
}
