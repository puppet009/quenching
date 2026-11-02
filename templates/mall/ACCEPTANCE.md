# mall 服务器验收清单

状态：加载配置和本地检查已准备，待服务器构建与部署验收。本地未构建 mall 镜像或运行项目容器。
部署和验收由项目负责人在服务器上完成，以下项目不能根据源码阅读直接标记通过。

| 验收项 | 状态 | 结果与证据 |
| --- | --- | --- |
| 上游固定 commit、文件完整性与许可证保留 | 已核验 | 见 SOURCE.md 与 upstream/*.source.json；不等同许可证法律审查 |
| 实际模板被 Registry List/Get 发现且 ChartPath 有效 | 本地通过 | `go test ./templates -run TestMall` |
| Chart 六服务绑定、六个 PVC、有效资源限额、镜像覆盖 | 本地通过 | Helm 离线渲染测试；应用限额2300m/3712Mi，非实际占用 |
| 终端源码目录、文件同步与源码离线重建 | 待服务器验证 | 本地脚本/浏览器回归通过；mall-0.2.0 需按源码工作区升级说明构建并新建班次 |
| Chart 语法和服务器脚本语法 | 本地通过 | `helm lint --strict`、`bash -n` |
| portal 沙箱支付密钥从生成的 Secret 注入 | 待验证 | 2026-10-27 补齐 SANDBOX_PAYMENT_SECRET 引用；需新班次验证应用启动，见 SERVER_DEPLOY.md |
| 加载阶段支付路径判断 | 本地通过 | 独立 JDK 路径测试；后端实际 HTTP503 仍需服务器验证 |
| 前端创建沙箱可等待部署，普通请求保持原超时 | 本地通过 | `npm test` 真实 HTTP 延迟测试 |
| 后端构建及必要服务启动 | 待验证 | |
| 商城前端构建及与后端固定版本兼容 | 待验证 | |
| 新沙箱初始化商品与库存，模拟用户真实注册和登录 | 待验证 | |
| 浏览、加购、下单、查单及订单落库 | 待验证 | |
| 库存锁定、支付扣减、取消释放及数据一致性 | 待验证 | |
| 支付等外部依赖使用沙箱模拟服务，网络出口隔离 | 待验证 | |
| 支付成功、失败、超时、延迟与重复回调验证 | 待验证 | |
| 正常业务与模拟负载下的资源预算实测 | 待验证 | |
| 后台绑定故障的真实来源及逐条触发、修复、恢复和清理 | 待验证 | |
| 同班次动态挑战保留修改，支持的组合分别验证 | 待验证 | |
| 模拟行为、学员操作、变更及故障证据保存供复盘 | 待验证 | |
| 班次结束销毁数据和修改，两次新班次无残留 | 待验证 | |

每条通过项应补充服务器环境、固定版本、执行命令、业务断言结果和证据位置。
加载验收清单已启用，完整准入前不正式向学员开放，不将待验证故障展示为可训练范围。

## 导演业务协议（待服务器验证）

平台命令引擎读取 adaptation/behavior-protocol.json；当前声明真实注册、登录、浏览、购物车、下单与订单归属验证。账号由引擎每班次注册；已声明沙箱支付能力，但不允许把 HTTP fixture 或接口加载当作实际交易验收。服务器需验证商品 SKU 库存、地址、订单及新账号随 namespace 重建清空。

支付验收：构建新版 portal/mysql/frontend 镜像并新建 namespace；30-sandbox-payments.sql 必须存在。验证平台 success/decline/delay/duplicate 模式；重复签名回调不得再次扣库存；金额/账号不匹配、过期/伪造签名及结束后的付款均拒绝；公开 /api/sandbox/payment/、/api/alipay/ 和 /api/order/paySuccess 保持阻断。已有数据库不会自动执行新 init SQL，按重建沙箱验收。

## 大脑故障准入与执行权限

服务器验收单项故障时，除 observed/repaired/revoked/cleanup/validated_at 外，在绑定 evidence 中记录内容服务 GET `/api/v1/templates/mall` 返回的 `artifact_digest` 为 `template_digest`。源码、适配或 Helm 改动都会使旧验收失效。首批 8 项候选仍未在真实服务器通过验收，不自动启用。

ChaosBlade agent 和 operator 必须由服务器预先安装；默认 `faultExecution.enabled=false`，不会授予执行身份。完成安装后在部署 values 中启用，并填写实际 agentNamespace/operatorName/agentPodSelector/container/bladePath。执行权限仅在 agent namespace 的 Role 中授予 pods/exec create；每个沙箱由 provisioner 单独创建 pods get/list 的 RoleBinding。已有沙箱再次调谐时补齐绑定。

内部 POST `/internal/v1/faults/commands`、GET `/internal/v1/faults/commands/:command_id` 以及 GET `/internal/v1/faults/trainable/mall` 使用 X-Internal-Token。命令先持久化再执行；响应丢失后查状态，unknown 绝不重注入，需凭实验 UID/节点审计确认并清理。unknown 且 UID 未知时保留未确认状态，由服务器管理员检查 ChaosBlade；不能假定自动到期代表修复。

两故障叠加通过管理员 `/api/admin/v1/defects/combinations` 配置，必须提供当前两故障与模板版本对应的 acceptance_digest，以及 baseline/combined/recovery/isolation/source 五项 evidence_json 证据。最多两项；未验收组合拒绝执行。通过单项与叠加演练验证注入、实际异常、修复、撤销和关班清理后再验收。


## 训练大脑验收（2026-10-05）

详见 [实训验证指南](../../docs/testing.md)。本地检查不代表真实模型、集群或交易验收通过；候选故障保持验收门禁。
