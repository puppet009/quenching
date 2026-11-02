# 故障注入运行时 · 云端验证手册（T2-13 / T3-12 / T1-26 / T1-27）

> 适用范围：fault-engine 运行时接线（剧本自动编排、临时注入、kubectl exec 通道、Prometheus 判定）
> 与 training-service 踩坑后果传导（次生故障注入、会话结束撤销钩子）。
>
> 约定：**本地不构建、不跑测试、不起服务**；本手册全部步骤均在云服务器 / 云上 Kubernetes 集群执行。
> 所有接口走内网、不带鉴权；统一响应范式 `{"code":0,"msg":"ok","data":...}`。

---

## 0. 验证范围与责任边界

两条注入链路，互不重复：

| 链路 | 来源标记 | 触发方 | 撤销方 |
| --- | --- | --- | --- |
| 剧本链路（T2-13） | `experiments.source='scenario'` | 实训开始时由编排流程调 fault-engine `POST /sessions/start` | 恢复阈值达成自动撤销 / 会话结束 `POST /sessions/:id/stop` |
| 临时链路（T3-12） | `experiments.source='consequence'` | training-service 用户踩坑采纳错误建议后调 `POST /inject` | 会话结束钩子先撤本服务登记的次生故障，再调 `stop` 兜底 |

执行通道只有一条：fault-engine 用 `kubectl exec` 进入 ChaosBlade DaemonSet agent Pod 执行
`blade create/destroy`。fault-engine 不直接连业务 Pod。

---

## 1. 环境前置清单（必须全部就绪）

### 1.1 Go 工具链（仅云服务器上需要，用于跑测试/构建）

```bash
go version   # 期望 go1.27.x（与 services/fault-engine/Dockerfile 的 golang:1.27-alpine 对齐）
```

### 1.2 数据库迁移工具

仓库镜像未打包 `migrate` 二进制（见 `deploy/helm/values.yaml` 的 `migration.enabled: false` 注释）。
二选一：

- 云服务器安装 [golang-migrate](https://github.com/golang-migrate/migrate)（要求 ≥ 4.15，支持 `-x-migrations-table`）；
- 或用官方镜像一次性执行：`migrate/migrate:v4.17.1`。

### 1.3 ChaosBlade agent DaemonSet

fault-engine 的配置默认假设（`internal/blade/cloudexec.go` 与 `configs/config.yaml`）：

- agent 以 DaemonSet 部署在命名空间 `chaos`，标签 `app=chaosblade-agent`；
- Pod 内容器名 `chaosblade`，blade 可执行路径 `/opt/chaosblade/blade`。

> 注意：代码选择的是 **agent Pod 内的 blade CLI**（`kubectl exec` 方式），不是 ChaosBlade Operator 的 CRD 方式。
> 云上若采用 Operator 发行版，必须确认其 Pod 内确实带有可交互的 `blade` 二进制，并核对上述命名空间/标签/容器名/路径，
> 与 `config.yaml` 的 `chaos` 段保持一致。

就绪检查（在有 kubeconfig 的云服务器上）：

```bash
# 每个节点都应有一个 agent Pod 且 Running
kubectl get pods -n chaos -l app=chaosblade-agent -o wide

# 手工验证 blade 可执行（把 <agent-pod> 换成上一步输出的 Pod 名）
kubectl exec -n chaos -c chaosblade <agent-pod> -- /opt/chaosblade/blade version
```

### 1.4 fault-engine 镜像内必须有 kubectl（当前镜像缺口，需云上处理）

当前 `services/fault-engine/Dockerfile` 运行阶段基于 `alpine:3.20`，**只装了 ca-certificates/tzdata，未安装 kubectl**；
而 `CloudExec` 是调用容器内 `kubectl` 可执行文件（`chaos.kubectl_path`，默认 `"kubectl"`）。
因此直接用现有镜像部署，任何注入都会在执行通道这一步失败。云上必须先补齐，二选一：

- **方案 A（推荐）**：补 Dockerfile，运行阶段安装 kubectl，例如：
  ```dockerfile
  RUN apk add --no-cache ca-certificates tzdata kubectl
  ```
  （alpine 仓库的 kubectl 版本需与集群版本兼容；如不兼容，从官方发布物拷贝固定版本二进制。）
- **方案 B**：云上构建时换用含 kubectl 的基础镜像 / init 容器注入二进制。

### 1.5 fault-engine Pod 的 RBAC（当前 Helm 缺口，需云上补）

`deploy/helm/templates/deployment.yaml` 目前只给 sandbox-service 设置了 `serviceAccountName`，
**fault-engine 用默认 ServiceAccount 运行，没有任何 exec / 读 Pod 权限**。`CloudExec` 需要：

- `get pods`（解析目标 Pod 所在 Node）；
- `list pods`（在 chaos 命名空间按选择器找 agent、按 node 过滤）；
- `create pods/exec`（对 agent Pod 执行命令）。

参考授权（最小权限，agent 命名空间按实际替换 `chaos`）：

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: fault-engine
  namespace: <平台命名空间>
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: fault-engine-chaos-exec
  namespace: chaos
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["pods/exec"]
    verbs: ["create"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: fault-engine-chaos-exec
  namespace: chaos
subjects:
  - kind: ServiceAccount
    name: fault-engine
    namespace: <平台命名空间>
roleRef:
  kind: Role
  name: fault-engine-chaos-exec
  apiGroup: rbac.authorization.k8s.io
---
# 若临时注入的目标 Pod 分布在任意沙箱命名空间（只给 namespace、按 Node 兜底也要先解析目标 Pod），
# 还需集群级“只读 Pod + 对 chaos 命名空间 exec”。MVP 可授予：
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fault-engine-runtime
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: fault-engine-runtime
subjects:
  - kind: ServiceAccount
    name: fault-engine
    namespace: <平台命名空间>
roleRef:
  kind: ClusterRole
  name: fault-engine-runtime
  apiGroup: rbac.authorization.k8s.io
```

> exec 权限只授予 `chaos` 命名空间即可（命令只 exec agent Pod）；跨命名空间只需要只读 `pods get/list`。
> 部署时让 fault-engine 的 Deployment 使用该 ServiceAccount。

### 1.6 Prometheus 可达，且逻辑指标名有对应 PromQL

剧本的恢复判定（如 `cpu_usage lte 70`）依赖 Prometheus 瞬时查询。fault-engine 配置段
`prometheus.metric_queries` 是“逻辑指标名 → PromQL”映射，`{{namespace}}` 会被替换成目标命名空间；
**未配置映射的逻辑指标会退化为裸指标名查询**（`cpu_usage{namespace="ns"}`），这在 kube 体系下通常查不到数据，
导致恢复判定 fail-closed（缺数据不判恢复）。云上必须按实际指标名补齐映射，并保证 `by (service)`/`by (pod)`
的标签里带有 service 名（判定器按 `service|metric` 对齐，服务名标签按 `service`→`app`→`container` 顺序取）。

连通性（在 fault-engine Pod 内或同命名空间临时 Pod 内）：

```bash
# 把 <ns> 换成沙箱命名空间，验证 cpu_usage 实际有序列返回
kubectl run -it --rm curlcheck --image=curlimages/curl --restart=Never -- \
  -G 'http://prometheus-k8s.monitoring.svc:9090/api/v1/query' \
  --data-urlencode 'query=avg(rate(container_cpu_usage_seconds_total{namespace="<ns>",container!=""}[1m])) by (service)'
```

---

## 2. 配置核对

### 2.1 fault-engine `config.yaml`（容器内 `/app/configs/config.yaml`）

| 配置键 | 含义 | 云上取值建议 |
| --- | --- | --- |
| `server.addr` | 监听地址 | `:8088` |
| `db.dsn` | PostgreSQL DSN | 用环境变量 `DB_DSN` 覆盖（Viper 支持 `.`→`_` 自动映射） |
| `chaos.kubectl_path` | kubectl 可执行文件 | 容器内已装则保持 `kubectl` |
| `chaos.kubeconfig` | kubeconfig 路径 | 容器内**留空**，走 ServiceAccount 默认凭证 |
| `chaos.agent_namespace` | agent 命名空间 | 与 1.3 一致（默认 `chaos`） |
| `chaos.agent_pod_selector` | agent 标签选择器 | 默认 `app=chaosblade-agent` |
| `chaos.container` | agent 内容器名 | 默认 `chaosblade` |
| `chaos.blade_path` | blade 路径 | 默认 `/opt/chaosblade/blade` |
| `prometheus.url` | Prometheus 地址 | 集群内 Service DNS |
| `prometheus.metric_queries` | 逻辑指标→PromQL | **必须按云上实际指标名核对/补齐** |
| `runtime.default_namespace` | 注入目标命名空间兜底 | 显式配置为实训沙箱命名空间，避免请求漏带时空值报错 |
| `runtime.poll_interval_seconds` | 指标轮询周期 | 默认 5 |
| `runtime.stable_required_seconds` | 恢复稳定窗口 | 默认 30 |

> Helm 提示：`deploy/helm/templates/configmap.yaml` 的 `quenching.renderConfig` 目前**没有渲染
> fault-engine 的 `chaos/prometheus/runtime` 段**，也没渲染 training 的 `fault_engine` 段。
> 若通过现有 Chart 部署，生成的 config.yaml 会缺这些键。云上需扩展该模板与 `values.yaml`，
> 或用环境变量 / 自定义 ConfigMap 覆盖后再挂载。

### 2.2 training-service `config.yaml`

| 配置键 | 含义 |
| --- | --- |
| `fault_engine.base_url` | fault-engine 内网地址，如 `http://fault-engine:8088`。**留空时次生故障只登记不注入**，启动日志会打印“fault_engine.base_url 未配置”警告，会话结束钩子也仅做本地清理 |
| `fault_engine.default_namespace` | 临时注入缺省命名空间，建议与沙箱命名空间一致 |
| `fault_engine.default_pod` | 目标 Pod 全局兜底，通常留空 |

配置加载支持环境变量覆盖，例如 `FAULT_ENGINE_BASE_URL`、`FAULT_ENGINE_DEFAULT_NAMESPACE`。

---

## 3. 数据库迁移（顺序：先 fault-engine，再 training）

两服务共用同一个库 `quenching`，但各自使用独立迁移表
（`schema_migrations_fault_engine` / `schema_migrations_training_service`），
**必须用 `-x-migrations-table` 区分**，否则版本号互相顶掉、迁移被静默跳过。

在仓库根目录（云服务器，把 `$DB_DSN` 换成云上 DSN）：

```bash
# fault-engine：本次涉及 000005（experiments 表）、000006（修正 easy 种子 kind）
migrate -path services/fault-engine/migrations \
  -database "$DB_DSN" \
  -x-migrations-table schema_migrations_fault_engine up

# training-service：本次涉及 000012（ai_consequence_injections 表）
migrate -path services/training-service/migrations \
  -database "$DB_DSN" \
  -x-migrations-table schema_migrations_training_service up
```

说明：

- `000006_fix_easy_seed_kind` 是幂等数据修复（down 为 no-op），新环境执行也安全；
  它把 easy 种子里错误的故障类型改回 `cpu`/`disk`（修复前会在注入时被“该故障类型暂不支持注入”拒绝）。
- 也可直接用根 Makefile 一键全量：`DB_DSN=$DB_DSN make migrate`（它会遍历全部服务并各自指定迁移表）。

迁移结果核验：

```sql
-- fault-engine
SELECT version, dirty FROM schema_migrations_fault_engine;          -- 期望 version>=6, dirty=f
SELECT count(*) FROM defects WHERE is_active = true;                 -- 应有种子剧本
SELECT to_regclass('experiments');                                   -- 期望 experiments

-- training
SELECT version, dirty FROM schema_migrations_training_service;       -- 期望 version>=13, dirty=f（000012 已含）
SELECT to_regclass('ai_consequence_injections');                     -- 期望 ai_consequence_injections
```

---

## 4. 单元测试（云上执行，本地不跑）

这些是纯逻辑测试（fake executor / fake fetcher / httptest），不依赖 Kubernetes、blade、真实数据库。

```bash
# fault-engine：外部测试包（可导 internal），含 RuntimeService 全链路 + blade 命令构造 + 剧本/调度
cd services/fault-engine
go test ./... -race -count=1

# training-service：HTTP 客户端契约 + finish 钩子 handler + 踩坑后果编排
cd ../training-service
go test ./... -race -count=1
```

重点关注的新增测试：

- fault-engine `tests/runtime_service_test.go`：临时注入（成功/命名空间回落/参数校验/失败落 `failed`）、
  按 uid 撤销（成功/幂等/不存在/跨会话隔离）、`StopSession` 补偿与幂等、剧本启动（延迟期零注入、登记 `created`）、
  重复会话拒绝（悬挂登记行落 `failed`）、注入历史。
- training-service `internal/service/consequence_http_client_test.go`：与 fault-engine 的 JSON 契约
  （注入/撤销/停止、业务错误透传、命名空间回落、尾斜杠规整）。
- training-service `internal/handler/runtime_test.go`：finish 钩子双路撤销、stopper 为 nil、
  两路错误聚合返回 500、非法会话 ID 400。

---

## 5. 服务启动与健康检查

```bash
# 构建镜像（构建上下文必须是仓库根，Go 服务用 replace 指向 packages/*）
docker build -f services/fault-engine/Dockerfile  -t quenching/fault-engine:dev .
docker build -f services/training-service/Dockerfile -t quenching/training-service:dev .
```

部署后：

```bash
kubectl -n <平台命名空间> rollout status deploy/fault-engine
kubectl -n <平台命名空间> rollout status deploy/training-service

# 健康检查（fault-engine / training 均暴露 GET /health）
kubectl -n <平台命名空间> exec deploy/fault-engine -- wget -qO- http://127.0.0.1:8088/health
kubectl -n <平台命名空间> exec deploy/training-service -- wget -qO- http://127.0.0.1:8086/health
```

启动日志检查：

- fault-engine 不应出现连接数据库 / Prometheus 报错；
- training-service 若未配置 `fault_engine.base_url`，会打印“次生故障注入与剧本编排通道不可用”警告——
  端到端验证前必须确认该警告**没有**出现。

---

## 6. kubectl exec 通道连通性（先于业务用例）

在 fault-engine Pod 内直接验证 kubectl 与默认凭证（方案 A 装的是 kubectl）：

```bash
kubectl -n <平台命名空间> exec deploy/fault-engine -- kubectl get pods -n chaos -l app=chaosblade-agent
```

能列出 agent Pod 即说明：二进制存在、ServiceAccount 凭证有效、list 权限到位。
随后用最小、无害的 blade 查询验证 exec 子资源权限：

```bash
kubectl -n <平台命名空间> exec deploy/fault-engine -- \
  kubectl exec -n chaos -c chaosblade <agent-pod> -- /opt/chaosblade/blade status
```

> 若这一步报 `forbidden: pods/exec`，说明 1.5 的 RBAC 未生效；报 `unable to find a pod with name ...`
> 说明 agent 命名空间/标签/容器名与配置不一致。先修好再继续。

---

## 7. fault-engine 运行时接口逐项验证

以下用集群内临时 curl Pod 调用（替换 `<FE>=http://fault-engine:8088`、`<NS>=沙箱命名空间`、`<POD>=业务Pod`）。

### 7.1 临时注入 + 按 uid 撤销（T3-12 引擎侧）

注入 CPU 满载：

```bash
curl -sS -X POST "$FE/api/v1/runtime/inject" -H 'Content-Type: application/json' -d '{
  "session_id": 9001,
  "kind": "cpu",
  "namespace": "<NS>",
  "pod": "<POD>",
  "cpu_percent": 80
}'
```

期望：HTTP 200，`code=0`，`data.status="running"`，`data.experiment_uid` 非空，
`data.source="consequence"`，`data.target` 形如 `<NS>/<POD>`。

落库核验：

```sql
SELECT id, session_id, source, kind, status, experiment_uid, namespace, pod
FROM experiments WHERE session_id = 9001 ORDER BY id;
```

同时在 agent 上确认 blade 实验真实存在：

```bash
kubectl exec -n chaos -c chaosblade <agent-pod> -- /opt/chaosblade/blade status
```

按 uid 撤销（用上一步返回的 uid）：

```bash
curl -sS -X POST "$FE/api/v1/runtime/revoke" -H 'Content-Type: application/json' -d '{
  "session_id": 9001, "uid": "<experiment_uid>"
}'
```

期望 `code=0`；再次撤销应**幂等成功**；仓储该行变为 `revoked`。

异常分支：

| 用例 | 请求 | 期望 |
| --- | --- | --- |
| 缺会话 ID | `session_id=0` | 400 / `code=10001`，msg 含“会话ID不能为空”，且无 blade 调用 |
| 不支持的类型 | `kind="threadpool"` | 400 / 10001，msg 含“该故障类型暂不支持注入” |
| 命名空间全空 | 不带 namespace 且全局默认也为空 | 400 / 10001，msg 含“目标命名空间不能为空” |
| 撤销不存在 uid | 随机 uid | 404 / `code=10002`，msg“注入实验不存在” |
| 跨会话撤销 | 用别的 session_id | 404 / 10002（应用层带会话归属校验） |

### 7.2 剧本启动（T2-13）

种子剧本（`defects` 表，`project_id=1`）可直接用于验证，例如
`cpu-stress-order-01`（delay 30s、CPU 100%、恢复阈值 `cpu_usage lte 70`、auto_revoke=true）。
> 用前确认其 `target.pod` 与云上沙箱真实工作负载匹配；也可在请求体里用 `pod` 覆盖目标 Pod。

```bash
curl -sS -X POST "$FE/api/v1/runtime/sessions/start" -H 'Content-Type: application/json' -d '{
  "session_id": 9002,
  "project_id": 1,
  "code": "cpu-stress-order-01",
  "namespace": "<NS>",
  "pod": "<POD>"
}'
```

期望：200 / `code=0`，返回的登记行 `status="created"`、`source="scenario"`；
**此时（delay 未到）不应有 blade 调用**，agent `blade status` 无新实验。

- 重复对同一 `session_id` 启动：第二次 400，msg 含“已在运行”；
  `experiments` 中该会话新增一行且落 `failed`（重复登记被标记失败，不影响第一条编排）。
- delay 到达后（约 30s + 一个轮询周期）：登记行转 `running`，agent 出现 blade 实验，业务 Pod CPU 上升。
- 恢复阈值满足并持续 `stable_required_seconds` 后（auto_revoke=true）：自动 `blade destroy`，登记行转 `revoked`。
  验证时可通过降低负载让 `cpu_usage` PromQL 结果低于 70。

触发类型覆盖（可用对应种子）：

| 触发类型 | 种子示例 | 观察点 |
| --- | --- | --- |
| delay | `cpu-stress-order-01` / `slow-query-order-01`(60s) | 到点才注入 |
| metric | `net-loss-product-01`（QPS>20 触发） | 用压测把 QPS 打过阈值才注入 |
| window | （自定义剧本）`trigger.type=window` | 窗口内评估才注入；错过窗口标记过期放弃 |

> metric/window 剧本的指标名（如 `qps`、`p99_latency`）必须在 `prometheus.metric_queries`
> 中配置可返回数据的 PromQL，且返回序列带 service 标签，否则既不触发也不判恢复。

### 7.3 会话结束停止（补偿撤销 + 幂等）

```bash
# 结束 9002：停止编排并撤销该会话全部仍活跃实验
curl -sS -X POST "$FE/api/v1/runtime/sessions/9002/stop"
# 期望 code=0

# 重复停止幂等
curl -sS -X POST "$FE/api/v1/runtime/sessions/9002/stop"
# 仍 code=0（无编排不报错）

# 对从未启动过的会话停止也安全
curl -sS -X POST "$FE/api/v1/runtime/sessions/9999/stop"   # code=0
curl -sS -X POST "$FE/api/v1/runtime/sessions/0/stop"      # 400 / 10001 “会话ID非法”
```

delay 未到就停止的场景：登记行尚为 `created`，编排 goroutine 退出时由收尾逻辑兜底标记为 `revoked`，
不应留下悬挂的 `created/running` 行：

```sql
SELECT status, count(*) FROM experiments WHERE session_id = 9002 GROUP BY status;
-- 停止并收尾后，不应再有 created / running
```

### 7.4 注入历史

```bash
curl -sS "$FE/api/v1/runtime/sessions/9002/experiments"
```

期望 `data` 按 id 升序返回该会话全部记录；查别的会话互不串数据；`id=0` 返回 400。

---

## 8. training-service 踩坑后果传导端到端（T3-12）

### 8.1 前置数据

- 存在可用的训练会话（`training_sessions`）与 AI 对话（`ai_conversations`）；
- 存在启用的坑点配置（`ai_pitfall_configs`），其 `pitfall_rules` 中 `worsen_fault=true`；
  未显式配 `consequence` 时，默认注入 CPU 满载；显式给类型但强度为 0 时按 severity 归一
  （如 CPU：low 60 / medium 80 / high 100；网络延迟：200/500/1000ms）。
- training 已配置 `fault_engine.base_url` 且能连通。

### 8.2 触发路径（真实对话链路）

1. 创建对话：`POST /api/v1/ai/conversations`（带用户身份头，body 带 `session_id`）。
2. 走到坑点后，用户对错误建议给出“采纳”性质的回复：
   `POST /api/v1/ai/conversations/:id/messages/stream`（SSE）。
   首次判定采纳且 `worsen_fault=true` 即联动一次注入。
3. 观察 training 日志，应出现“次生故障已注入（采纳错误建议，监控应见恶化）… kind=… uid=…”。

两侧落库核验：

```sql
-- training 侧：同一坑点消息至多一行（幂等唯一键）
SELECT id, session_id, pitfall_message_id, kind, cpu_percent, mem_percent,
       delay_millis, loss_percent, status, experiment_uid, fail_reason
FROM ai_consequence_injections WHERE session_id = <SID>;
-- 成功：status='injected'，experiment_uid 非空
-- fault-engine 不可用：status='pending'/'failed'，fail_reason 有原因，且训练不被阻断

-- fault-engine 侧：对应 consequence 实验
SELECT source, kind, status, experiment_uid FROM experiments
WHERE session_id = <SID> AND source = 'consequence';
```

幂等性：对**同一条坑点消息**再次触发采纳，不应产生第二行（`uq_consequence_pitfall_message` 唯一键），
日志出现“次生故障已登记，跳过重复恶化”。

### 8.3 会话结束撤销钩子

云端沙箱释放流程在会话完成 / 中止 / 超时时回调 training：

```bash
curl -sS -X POST "$TR/api/v1/runtime/sessions/<SID>/finish"
# 成功：200 {"code":0,"msg":"ok"}
```

钩子做两件事（针对不同来源，互不重复）：

1. 撤销 training 登记的全部 `injected` 次生故障（逐条 blade destroy + 标记 `revoked`）；
2. 调 fault-engine `POST /sessions/<SID>/stop` 停剧本编排并撤销剧本故障。

核验：

```sql
-- training：无残留 injected
SELECT count(*) FROM ai_consequence_injections
WHERE session_id = <SID> AND status = 'injected';   -- 期望 0

-- fault-engine：无残留活跃实验（两类来源）
SELECT count(*) FROM experiments
WHERE session_id = <SID> AND status IN ('created','running');  -- 期望 0
```

幂等与失败处理：

| 场景 | 期望 |
| --- | --- |
| 重复回调 finish | 200，重复撤销安全（已 revoked 的不再生效） |
| 非法 ID（abc/0/-3） | 400 / `code=10001`，msg“会话ID非法”，不产生任何调用 |
| 其中一路撤销失败 | 仍尝试另一路；返回 500 / `code=50000`，msg 前缀“会话故障撤销未完全成功”，云端应记录并重试（接口幂等） |
| 未配置 fault-engine（stopper=nil） | 仅撤次生故障，仍返回 200 |

> 钩子使用独立的 30s 超时上下文（不绑定可能已断开的 HTTP 请求）；单次运行时调用客户端超时为 15s。

---

## 9. 崩溃 / 退出兜底

fault-engine 进程收到 SIGINT/SIGTERM 时会在优雅关停（15s）内调用注册中心 `StopAll`，
撤销本实例管理的全部活跃实验。验证：

```bash
kubectl -n <平台命名空间> rollout restart deploy/fault-engine
# 重启前若有 running 实验，重启后应已 destroy；experiments 终态不应长期停留 running
```

> 注意：进程被强杀（SIGKILL/节点故障）时来不及走 StopAll，这类悬挂实验依赖会话结束 `stop`
> 或人工 `blade destroy <uid>` 兜底——`experiments.experiment_uid` 与 `command` 列保留了恢复凭证与审计命令。

---

## 10. 验收标准（逐项打勾）

- [ ] 两服务迁移版本正确、无 dirty 锁；`experiments` 与 `ai_consequence_injections` 表存在。
- [ ] `go test ./... -race`（fault-engine、training-service）在云上全绿。
- [ ] fault-engine 容器内 kubectl 可用、RBAC 允许 list pods + create pods/exec（第 6 节通过）。
- [ ] 临时 inject/revoke：blade 实验真实创建/销毁，仓储状态正确，异常分支返回码符合预期。
- [ ] 剧本 start：delay 未到零注入；到点注入；恢复阈值稳定后自动撤销；重复会话拒绝且悬挂行 failed。
- [ ] stop 幂等，无 `created/running` 残留。
- [ ] 踩坑采纳后两侧落库一致（training `injected` + fault-engine `consequence/running`），同坑点不重复恶化。
- [ ] finish 钩子双路撤销、幂等、失败聚合为 500/50000 可重试。
- [ ] fault-engine 重启后无长期残留 running 实验。

---

## 附：接口速查

**fault-engine（默认 :8088，前缀 `/api/v1/runtime`，内网无鉴权）**

| 方法 | 路径 | 关键字段 |
| --- | --- | --- |
| POST | `/sessions/start` | `session_id` `project_id` `code` `namespace?` `pod?` |
| POST | `/sessions/:id/stop` | 路径参数会话 ID |
| POST | `/inject` | `session_id` `kind` `namespace?` `pod?` `process?` `network_interface?` `delay_millis` `loss_percent` `cpu_percent` `mem_percent` `disk_percent` |
| POST | `/revoke` | `session_id` `uid` |
| GET | `/sessions/:id/experiments` | 路径参数会话 ID |
| GET | `/health` | 健康检查 |

错误码：`10001` 参数/业务错误（400），`10002` 不存在（404：剧本不存在 / 注入实验不存在）。

**training-service（默认 :8086）**

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/runtime/sessions/:id/finish` | 会话结束撤销钩子（双路、幂等；失败 500/50000） |
| GET | `/health` | 健康检查 |
