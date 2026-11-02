# 测试与服务器验收

当前为 Alpha，真实模型、数据库、集群、mall 交易与故障闭环仍待服务器验收。源码检查、fixtures 和脚本通过只证明各自覆盖的行为；首批八项故障目录不等于八项可训练故障，未验收项保持禁用。

部署按[服务器部署手册](../deploy/server/README.md)、[运维手册](../deploy/server/服务器运维手册.md)执行；模板步骤与逐项结果见 [mall 部署](../templates/mall/SERVER_DEPLOY.md)、[mall 验收清单](../templates/mall/ACCEPTANCE.md)和[故障目录](mall-first-batch-faults.md)。

## 本地检查

从当前提交安装各模块声明的依赖，以下命令在仓库根目录的 Bash 环境执行。Go 工作区包含多个模块，逐模块运行：

```bash
set -euo pipefail
python scripts/check_structure.py
python scripts/qa/domain-boundaries.py
bash scripts/qa/migration-order.sh
for module in packages/go-common packages/proto services/*; do
  if [ -f "$module/go.mod" ]; then (cd "$module" && go test ./...); fi
done
(cd services/ai-service && APP_ENV=development LLM_MOCK=true python -m pytest tests/)
pnpm --filter @quenching/web test
pnpm --filter @quenching/admin test
pnpm --filter @quenching/web build
pnpm --filter @quenching/admin build
for test in templates/mall/tests/*.test.sh; do bash "$test"; done
```

- Helm 渲染测试需配置 `HELM_BIN`；未配置而出现 SKIP 时不能声称 Chart 已验证。迁移顺序脚本使用模拟 kubectl，不执行真实迁移。
- PostgreSQL 并发、租约、审计、记忆和复盘集成测试需独立测试库：配置 `TEST_DATABASE_URL`，重新运行相关 Go 模块并加 `-count=1 -v`；大脑仓储和关班评分另用 `BRAIN_TEST_DATABASE_URL` 及 `go test -tags=integration ./internal/repository -count=1 -v`（在 training-service 目录）。逐项确认没有 SKIP；测试可能创建并删除 schema，禁止指向生产库。
- `scripts/qa/operation-evidence-browser.cjs` 和 `resource-pool-browser.cjs` 需相应前端开发服务及 Playwright；它们拦截 API，验证界面行为，不证明真实后端、模型或集群可用。
- 记录本次提交、命令、环境、退出码和跳过项；旧验证记录不能代替当前提交的检查。

## 服务器准备

- 升级前暂停新班次、结束并清理存量环境，备份数据库；按部署手册完成各域全部顺序迁移、RBAC、PriorityClass、路由及镜像更新，再恢复入口。平台与模板使用可追溯版本，各节点可获取同一镜像；不混用旧活动班次与新故障执行器。
- 保持既有 JWT、数据库和密钥配置，各服务使用一致的非空内部 token；内部执行、冻结和审计接口仅限受控服务网络，网关用户接口必须鉴权。自定义路由整表覆盖时保留 `/api/v1/sessions` 到 training-service。
- 使用独立验收用户/班次及沙箱支付，确认 StorageClass 为 Delete、动态 PVC 可用，namespace 销毁后 PV 和后端数据确实回收。
- 模型设为生产配置、`LLM_MOCK=false`、`LLM_MODEL=qwen3.8-flash`，通过 Secret 注入凭证。负责人可运行下列四次真实计费探测；成功仅证明适配器连接，不代替 Agent、审批和报告质量验收：

```bash
python scripts/acceptance/brain-qwen-smoke.py --allow-paid
```

- 核对任务使用实际模型及有效 usage，模型不可用不得回退 mock/其他模型；助手 SSE 不输出推理正文。检查真实工具往返、拒绝审批不执行，以及非法 JSON、跨班次证据被拒绝。

## 首次实训与故障准入

1. 新建 mall 班次，等待业务服务和行为引擎就绪后才开始计时；验证真实注册、登录、浏览、加购、下单、订单归属、库存和沙箱支付。用订单/数据库结果核对账号、金额与库存，不以 HTTP 200 代替业务成功；runner 重启不另建账号或订单。
2. 分别测试支付成功、拒绝、延迟与重复回调；重复回调只推进一次原事务，错误账号/金额/签名、过期及跨班次回调被拒绝。公网支付路径被阻断，沙箱不能访问真实支付系统。
3. 默认 `faultExecution.enabled=false`。先验证真实 ChaosBlade operator/agent、目标节点和最小 Pod/agent exec 权限；不降级为主机注入。无已验收故障时明确显示不可训练，不伪造成功。
4. 独立验收每项故障的事故来源、模板差异、异常、有效修复、业务恢复、撤销和清理；绑定 observed/repaired/revoked/cleanup/validated_at 及当前 `template_digest`，再设 verified/active。源码、适配、Chart 或运行摘要改变后重新验收。
5. 新班次先通过交易基线，再注入已验收 easy 故障，记录 command_id、实验 UID、Pod UID/节点。模板摘要或目标已变化时拒绝；单项通过不能代替组合验收。
6. 修复需平台观察到有效改动与故障有效期内业务恢复；到期、撤销、命令退出 0 或学员自述不能单独证明修复。助手代改保留 assisted_repair，证据不明不晋级。确认原实验撤销后才追加挑战，班次 ID 不变。

## 源码工作区与重置

- 按[源码工作区升级与验收](../deploy/server/源码工作区与终端目录升级.md)检查前端 `/workspace/mall-app-web`、Java `/workspace/mall` 和 `TRAINING.md`；终端切换目录时文件列表同步，编辑器草稿和不同容器目录不混入。
- 修改真实源码后运行 `training-rebuild`，等待健康检查并验证页面/接口变化。编译错误返回非零且保留上次发布；修复后可重建，删除/改名文件不残留旧产物。离线构建只覆盖镜像已缓存依赖。
- 容器重建后同班次源码、配置与成功发布结果保留；结束后工作区、业务数据、账号、订单及模拟状态随独立 PVC 清除。新建两个班次复查初始源码和数据，无前班残留。

## 后台与权限

- 概览读取真实领域数据：零显示零、未评分显示暂无数据、服务不可用显示错误；列表筛选重置页码，失败不冒充成功或沿用旧结果。
- 内容草稿/上下线和故障编辑刷新后持久；普通用户不能写管理接口，content_editor 不能读取反馈或释放沙箱。修改故障使旧验收失效，未验收故障禁止直接启用。
- 同用户/班次反馈重复提交更新原记录，评分边界有效；他人班次拒绝。沙箱按 session_id 释放并轮询到 released，批量失败明确到具体班次；未采集指标显示未知。
- 伪造 `X-User-ID` 不改变 JWT 身份；权限服务不可用时拒绝管理操作。两个用户交叉访问班次、证据、助手和报告均拒绝，私有根因与未来挑战不出现在学生接口。

## 助手记忆与隔离

- 收起、刷新、切换班次及服务重启后恢复同一规范对话，分页消息不重不漏、问题先于回答；恢复失败禁止新建孤立对话。准备中不可提问，结束后只读，旧班次请求/审批结果不进入新班次。
- 累计至少 65 条消息，早期包含失败尝试和约束；后续追问仍能检索原文并按真实 message/evidence 引用复查。公开操作按可靠证据归属挑战，用户说法、助手建议与平台成功观察分开。
- 摘要失败、非法引用、超预算、过期租约或旧版本 CAS 保留旧快照与水位并提示降级；并发实例只允许一个有效回复租约。中断后工具修改不自动重放。
- 对超过 8KiB 的脱敏 UTF-8 原文按字节读取到尾部；截断不冒充完整覆盖。新旧消息、检索、摘要和模型输入均检查密码、JWT、授权头脱敏；跨班次引用、隐藏根因不可读。

## 操作证据与执行

- 在“命令执行”测试 stdout、stderr+退出 0、退出 7；stderr 不等于失败。取消、断网、刷新和重启后查询原执行，不能重新执行；unknown 的 exit_code 为 null，不展示成功或已回滚。
- 重试沿用同一幂等 key，相同 key 不同参数拒绝；两个执行实例最多执行一次。注入响应丢失先查 command_id；UID 未知时保留待人工核对，不盲目重注入。
- 文件创建/改删/复制/重命名/权限/上传保留可得前后状态；前态或权限未知时标明未知，二进制和截断内容不冒充完整 diff。写入已生效但审计失败不自动重写。
- 工具步骤、审批和关联回复刷新可读；拒绝不执行，失效审批不可恢复执行，结束后禁止新审批。交互终端/vim/SQL 客户端仅有有限覆盖，原始输入正文不落库，长行和断连缺口必须显示。
- 按引用分页读到 stdout/result/before/after 尾部，核对中文边界、脱敏和权限；审计交付恢复后按事件 ID 去重，原文与事件原子入库再确认。

## 资源排队与故障冻结

- 后台资源池对照真实节点/Pod/Quota，区分 requests、limits、预留和实际使用；无 metrics 显示未知。新班次容量不足进入 queued、无 namespace 且不计时，恢复容量后自动部署；结束排队不得复活。
- 并发准入/伸缩不得重复消费预留；重启后核对既有 Helm release，不重复安装。残缺、pending 或 failed release 不冒充 ready，也不自动删除；先查 Helm/Pod Events，再运维修复或结束重建。
- 验证有完整新指标才调整、受 limits/副本策略与冷却窗口约束；先预留再更新，Pod 和 Quota 收敛后才释放预留。缺指标/容量/权限时不调整；手工资源调整需同步 Helm，不能假设具备自动伸缩的容量保证。
- 资源故障执行前持久化 hold，冻结本班次自动资源调整及平台 fault-engine 自动伸缩；两故障分别持有，最后一次确认撤销后才解冻。重启、超时、未知 UID 或清理失败均不自动解锁，其他班次继续运行。
- 伸缩未收敛或冻结服务不可用时拒绝新注入；已经开始后通信失败保留 unknown 与 hold。节点加入、镜像分发及本地卷限制按[运维手册](../deploy/server/服务器运维手册.md)执行。

## 结束与异步复盘

- 前端提交完最后一批操作并收到结束请求已接受后，返回实训历史页；资源回收和复盘继续在后台处理。历史页区分待处理、失败和完成，不让页面等待模型生成完毕才退出。
- 结束先冻结新输入/文件/审批，等待已有执行与审计，再撤销故障、销毁 namespace。撤销失败继续重试并显示失败；namespace 仍 Terminating 为 pending，不将 DELETE 接受当清理成功。
- 结束请求成功不等于清理或报告完成。交付中断/积压及不可达时应按封账期限产生含 audit_delivery_timeout/result_unconfirmed 的缺口快照；清理状态、证据覆盖与业务恢复独立展示，缺口不得变成已证实。
- 同班次只生成一份整场报告；中途完成挑战不生成正式报告。重复结束、队列重复投递、worker/模型中断后恢复同一冻结快照，hash 不变，旧租约不能覆盖产物或分数；迟到审计不改已冻结报告。
- 人工核对六章、七维能力/置信度、未解决项、事故来源、架构取舍与下一练习；长输入处理覆盖尾部，引用可读。观测事实有同班次证据，行业建议明确标识；缺少证据/基线不宣称晋级，保留原技术与协作分。
- 清理成功后确认 namespace/PVC/PV、runner 凭证和支付上下文清除，平台证据、对话、评估与报告仍可读；报告完成不能掩盖清理失败。

```bash
# 环境已设置 SESSION_ID、PLATFORM_URL、USER_ACCESS_TOKEN
bash templates/mall/scripts/verify-training.sh
EXPECT_CLOSED=true bash templates/mall/scripts/verify-training.sh
```

该脚本只读检查状态和 namespace。每项验收保留提交/镜像/模板摘要、session_id、command_id、时间、业务断言与脱敏证据位置，明确 live 或 fixture；交易、模型质量、故障撤销和物理卷回收均需各自证据。
