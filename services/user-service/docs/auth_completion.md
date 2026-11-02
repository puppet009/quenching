# 注册登录补全（T4-5）

当前 MVP 入口启用手机号/邮箱 + 密码注册登录及 Token 续期。短信验证码、邮件找回和登录限流有服务层实现/接口，但 `cmd/server/main.go` 尚未注入短信、邮件或限流服务，不能据此认为生产可用。

## MVP 注册策略

`configs/config.yaml` 的 `auth.registration_sms_required: false` 与 Helm 的 `services.user-service.registrationSMSRequired: false` 明确关闭手机号注册的短信验证。注册请求只需 `account`、`password`，可选 `nickname`；手机号与邮箱都会执行格式、密码强度、查重和密码哈希，成功后返回用户与令牌。当前不会验证手机号/邮箱归属。

`AUTH_REGISTRATION_SMS_REQUIRED` 可覆盖 YAML。缺少配置时入口和 `NewAuthService` 均保持要求手机号验证码的默认策略。未来必须先接入可用的 `SMSService` 和前端发送/输入验证码流程，再启用该设置；仅设置为 `true` 不会自动获得短信能力。严格模式不因未传验证码、未配置 provider、错误或过期验证码而放行。此设置不改变短信登录，也不为邮箱新增验证能力。

## 交付物

```
internal/
  service/sms_service.go  # 短信服务（mock + real 占位）
  service/login_limiter_service.go # 登录限流 + 异常告警
  service/reset_password_service.go # 密码重置（邮箱 + token）
  service/errors.go       # 补充错误定义
  service/auth_service.go # AuthService 扩展（选项模式注入能力）
  service/store.go        # UserStore 扩展（FindByID/UpdatePassword）
  handler/auth_handler.go # 8 个 HTTP 接口
tests/auth_test.go       # 服务层回归
tests/registration_policy_test.go # MVP 无验证码 HTTP 注册/登录与严格模式回归
```

## 4 大能力

### 1. 短信验证码（mock 开关）

**模式切换**：
- `MockSMSService` —— 开发/测试环境，直接返回验证码明文，当前入口未注入
- `RealSMSService` —— 生产环境占位，发送和校验均返回错误，必须实现真实短信网关后才能使用

**用途**：
- `register` —— 手机号注册
- `login` —— 短信免密登录
- `reset_pwd` —— 手机号重置密码
- `change_phone` —— 换绑手机

**特性**：
- 5 分钟有效期（可配置）
- 6 位数字验证码
- 一次性使用（验证后标记已使用）
- 按手机号 + 用途维度隔离

**接口**：
```
POST /api/v1/auth/sms/send        发送验证码
POST /api/v1/auth/login/sms       短信验证码登录
```

### 2. 找回密码（邮箱重置）

**流程**：
```
用户输入邮箱 → 发送重置邮件（含 token）
→ 用户点击链接 → 前端验证 token 有效性
→ 输入新密码 → 提交重置
```

**Token 特性**：
- 32 字节随机 token（hex 编码）
- 30 分钟有效期
- 一次性使用
- 与邮箱绑定

**安全设计**：
- 无论邮箱是否注册都返回成功（不泄露信息）
- token 消费后立即失效
- 新密码需满足强度校验

**接口**：
```
POST /api/v1/auth/password/reset-request   请求重置（发邮件）
GET  /api/v1/auth/password/reset/verify    验证 token
POST /api/v1/auth/password/reset           重置密码
```

### 3. Token 自动续期

**机制**：
- Access Token：短期有效（默认 2 小时）
- Refresh Token：长期有效（默认 7 天）
- Access Token 过期后，前端用 Refresh Token 静默换取新的 token 对
- 每次刷新都返回新的 Access + Refresh Token（滚动刷新）

**接口**：
```
POST /api/v1/auth/token/refresh   刷新 token
```

**请求方式**：
- Body: `{ "refresh_token": "xxx" }`
- 或 Header: `X-Refresh-Token: xxx`

### 4. 异常登录限流告警

**双维度限流**：
| 维度 | 默认阈值 | 窗口 | 锁定时长 |
|------|---------|------|---------|
| IP | 10 次 | 10 分钟 | 30 分钟 |
| 账号 | 5 次 | 15 分钟 | 30 分钟 |

**告警机制**：
- 警告级（warn）：失败次数达到阈值一半时
- 严重级（error）：触发锁定时
- 通过 `AlertCallback` 接入告警系统（飞书/钉钉/邮件）

**安全特性**：
- IP + 账号双维度独立计数
- 登录成功后清除失败记录
- 锁定时间友好提示（剩余几分几秒）
- 支持反向代理 IP 识别（X-Forwarded-For / X-Real-IP）

## 架构设计

### 选项模式注入

AuthService 使用选项模式（Option Pattern）注入扩展能力：

```go
svc := service.NewAuthService(store, jwt,
    service.WithSMS(smsSvc),
    service.WithLoginLimiter(limiter),
    service.WithResetService(resetSvc),
)
```

好处：
- 核心注册登录不依赖可选能力
- 各能力独立，可按需开启
- 便于单测（只注入需要的依赖）

### 错误码映射

| 错误码 | HTTP 状态 | 说明 |
|--------|----------|------|
| 10001 | 400 | 参数错误 / 密码强度 |
| 20002 | 401 | 账号或密码错误 |
| 20003 | 403 | 账号已封禁 |
| 20005 | 409 | 账号已注册 |
| 20006 | 400 | 验证码错误/过期 |
| 20007 | 503 | 短信发送失败 |
| 20008 | 429 | 登录失败次数过多 |
| 20009 | 401 | Refresh Token 无效 |
| 20010 | 400 | 重置链接无效 |

## 测试覆盖

20+ 测试用例，覆盖：
- 短信发送/验证/过期/一次性
- 短信登录/注册
- 登录限流（触发/剩余次数/锁定）
- Token 刷新（成功/失败）
- 密码重置完整流程
- 限流告警回调
- 登录成功清除记录

## 生产环境接入

### 短信服务

```go
// RealSMSService 目前仍是占位；须先实现真实发送和校验，再注入入口。
smsSvc := service.NewRealSMSService(
    os.Getenv("SMS_ACCESS_KEY"),
    os.Getenv("SMS_ACCESS_SECRET"),
    "淬火科技",  // 签名
    "SMS_12345", // 模板
)
```

### 邮件服务

实现 `service.ResetEmailSender` 接口，接入 SMTP 或邮件推送服务，再通过 `WithResetService` 注入入口。

### 告警回调

```go
limiter.SetAlertCallback(func(ctx context.Context, event service.LoginAlertEvent) {
    // 发送到飞书/钉钉/监控系统
    alerting.Notify(ctx, event)
})
```

### Token 存储

生产环境建议使用 Redis 存储：
- 短信验证码
- 密码重置 token
- 登录限流计数

替换内存实现为 Redis 实现即可，接口不变。
