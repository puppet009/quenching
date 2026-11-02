# 故障剧本管理后端（T4-8）

管理后台 API，支持故障剧本的 CRUD、启用禁用、排序调整、配置校验。

## 包结构

```
internal/model/defect.go         # Defect 模型（defects 表映射）
internal/repository/
├── defect_repo.go               # DefectRepository 接口 + 内存实现
└── errors.go                    # 仓储层错误定义
internal/service/defect_service.go # 业务逻辑 + 配置校验
internal/handler/defect.go       # HTTP 接口（管理后台）
tests/defect_management_test.go  # 单元测试
```

## 接口清单

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/v1/defects` | 剧本列表（分页+筛选+搜索） |
| POST | `/api/admin/v1/defects` | 创建剧本 |
| GET | `/api/admin/v1/defects/:id` | 剧本详情 |
| PUT | `/api/admin/v1/defects/:id` | 更新剧本（部分更新） |
| DELETE | `/api/admin/v1/defects/:id` | 删除剧本 |
| PATCH | `/api/admin/v1/defects/:id/active` | 启用/禁用 |
| POST | `/api/admin/v1/defects/reorder` | 调整排序（交换两个） |
| POST | `/api/admin/v1/defects/:id/validate` | 校验剧本配置合法性 |

## 列表查询参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| project_id | int | 是 | 项目ID |
| keyword | string | 否 | 关键词（名称/code/描述） |
| category | string | 否 | 分类筛选 |
| difficulty | string | 否 | 难度筛选（easy/medium/hard） |
| defect_type | string | 否 | 类型筛选（code/config/architecture） |
| is_active | bool | 否 | 是否启用（不传=全部） |
| page | int | 否 | 页码，默认 1 |
| page_size | int | 否 | 每页条数，默认 10 |

## 配置校验

创建/更新时，如果提供了 `amplifier_config`，会自动调用 `scenario.Parse` 校验配置合法性，确保剧本可执行。

额外提供 `/validate` 接口，可对已有剧本做完整校验，检查：
- 放大器配置（ChaosBlade 可执行性）
- 触发配置
- 表现配置
- 修复验证配置
- 基础字段完整性

## 设计要点

### 1. Repository 接口抽象

仓储层以接口形式定义，内置内存实现：
- 单测直接用内存实现，无需数据库
- 生产环境替换为 GORM/PG 实现，接口不变
- 便于未来切换存储方案

### 2. 配置校验复用 scenario 包

放大器配置的校验直接复用 `scenario.Parse()`：
- 保证管理后台写入的配置一定能被注入引擎解析
- 避免两套校验逻辑不一致
- 新增故障类型时只需更新 scenario 包

### 3. 部分更新（PATCH 语义）

`UpdateDefectReq` 使用指针字段，只更新非 nil 的字段：
- 前端可以只传要改的字段
- 避免全量覆盖导致意外丢失
- 支持细粒度的字段编辑

### 4. 软删除预留

数据库表设计有 `deleted_at` 字段，当前内存实现是硬删除，
切换到 PG 实现时改为软删除即可，接口不变。

### 5. 排序策略

- 新建剧本默认 sort_order = ID（保证顺序稳定）
- 列表默认按 sort_order 升序，同序按 ID 升序
- 排序调整采用交换两个位置的方式（前端拖拽交换场景）

## 错误码

| code | 说明 |
|------|------|
| 0 | 成功 |
| 10001 | 参数错误 / 业务校验失败 |
| 10002 | 剧本不存在 |
| 10003 | 剧本编码已存在 |

## 与前端对接（T2-27）

D4 做 T2-27 故障剧本管理前端时，直接对接以下接口：

| 页面 | 用到的接口 |
|------|-----------|
| 剧本列表页 | GET /defects（分页+筛选+搜索） |
| 新建剧本 | POST /defects |
| 编辑剧本 | GET /defects/:id + PUT /defects/:id |
| 剧本详情 | GET /defects/:id |
| 启用/禁用开关 | PATCH /defects/:id/active |
| 拖拽排序 | POST /defects/reorder |
| 配置校验提示 | POST /defects/:id/validate |

## 后续扩展

- 剧本导入/导出（JSON）
- 剧本复制（基于已有剧本快速创建）
- 剧本版本管理（变更历史+回滚）
- 剧本标签/分组
- 批量操作（批量启用/禁用/删除）
- 操作审计日志（谁在什么时候改了什么）
