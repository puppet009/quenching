# 难度参数系统（T3-30）

三档难度参数配置：简单 / 普通 / 困难。

## 包结构

```
internal/difficulty/
├── config.go      # 难度等级 + Config 结构 + 三档预设
└── service.go     # Service + ApplyToFaultSpec + JSON 序列化

internal/handler/
└── difficulty.go  # HTTP 接口（列表 + 详情）

tests/
└── difficulty_test.go  # 单元测试
```

## 三档参数对比

| 维度 | 简单 (easy) | 普通 (normal) | 困难 (hard) |
|------|------------|--------------|------------|
| 故障强度倍率 | 0.6× | 1.0× | 1.5× |
| 告警灵敏度阈值 | 0.3（灵敏） | 0.5（标准） | 0.8（迟钝） |
| 告警数量倍率 | 1.5× | 1.0× | 0.4× |
| 日志级别 | debug | info | warn |
| 日志详细度 | 3（最全） | 2（标准） | 1（简略） |
| AI 提示次数 | 8 次 | 5 次 | 3 次 |
| 渐进式提示级数 | 3 级 | 2 级 | 1 级 |
| 干扰项 | 无 | 无 | 有 |
| 干扰强度 | 0 | 0 | 0.6 |
| 时间限制 | 不限 | 不限 | 45 分钟 |

## 难度如何影响故障剧本

### 注入强度（正向倍率）

难度越高，故障越强：

```
实际烈度 = 基础烈度 × FaultIntensity
```

示例（基础 200ms 网络延迟）：
- 简单：200 × 0.6 = **120ms**（现象温和，容易恢复）
- 普通：200 × 1.0 = **200ms**（标准）
- 困难：200 × 1.5 = **300ms**（现象强烈，恢复难）

### 恢复阈值（反向调整）

难度越高，恢复阈值越严格（越难达标）：

```
实际阈值 = 基础阈值 × RecoveryStrictness
```

| 难度 | RecoveryStrictness | 含义 |
|------|-------------------|------|
| 简单 | 1.5 | 阈值放宽 50%，容易达标 |
| 普通 | 1.0 | 标准阈值 |
| 困难 | 0.7 | 阈值收紧 30%，很难达标 |

示例（基础 P99 恢复阈值 200ms）：
- 简单：200 × 1.5 = **300ms**（延迟降到 300ms 以下就算修好）
- 普通：200 × 1.0 = **200ms**（标准）
- 困难：200 × 0.7 = **140ms**（必须降到 140ms 以下才算修好）

### 其他维度

- **告警**：简单模式告警多且触发早，困难模式告警稀少且延迟触发
- **日志**：简单模式 debug 级别 + 全堆栈，困难模式 warn 级别 + 简略信息
- **AI 提示**：简单模式提示多且详细（三级），困难模式只有 3 次方向性提示
- **干扰项**：困难模式有无关告警和日志噪音，增加排查难度
- **时间限制**：困难模式 45 分钟限时，增加压力

## HTTP 接口

### GET /api/v1/difficulties

列出所有难度选项（前端下拉选择用）。

### GET /api/v1/difficulties/:level

获取指定难度的完整参数配置。

## 使用方式

### 1. 创建会话时指定难度

```go
// 在会话创建流程中
diffCfg, err := difficulty.GetConfig(req.Difficulty)
if err != nil {
    diffCfg = difficulty.MustGet("normal") // fallback 到普通
}
session.Difficulty = string(diffCfg.Level)
```

### 2. 注入故障前应用难度

```go
svc := difficulty.NewService()
result, err := svc.ApplyToFaultSpec(ctx, baseSpec, session.Difficulty)
if err != nil {
    return err
}
// result.Spec 是调整后的故障参数
// result.DiffCfg 是完整难度配置（可返回给前端）
```

### 3. 前端展示选项

调用 `GET /api/v1/difficulties`，返回三档配置，用户选择后将会话的 `difficulty` 字段设置为对应值。

## 设计要点

1. **纯配置，无外部依赖** —— 预设硬编码在包内，启动即可用，未来可扩展到配置中心
2. **不可变预设** —— Config 是值类型，`ApplyToFaultSpec` 返回新对象，不修改原始参数
3. **边界安全** —— 所有强度调整都有 min/max 限制，不会出现极端值
4. **接口一致** —— Handler 风格与 conversation_handler.go 一致
5. **可测试** —— Service 无状态，单元测试覆盖三档差异、边界、序列化

## 后续扩展

- 自定义难度（用户可调整参数）
- 难度等级更多档位（如"专家"）
- 从配置中心动态加载难度预设
- 难度与项目/剧本绑定（不同项目默认难度不同）
