# mall 首批故障目录与适配边界

这批配置覆盖 code / config / architecture / resource 四种类型和八个技术分类。配置存入 fault-engine 的 defects；模板通过 slug=mall 与 workload 绑定，不要求数据库中存在项目 1。

**全部默认禁用，未在真实服务器验收。** 有来源不等于 mall 天然存在对应漏洞；来源可以是官方复盘或用户真实 Issue，可信度与复现差异随配置保存。

| 编码 | 类型 / 分类 | 目标 | 当前状态 | 真实来源 |
| --- | --- | --- | --- | --- |
| mall-cpu-001 | resource / cpu | mall-portal | pending_validation | [Cloudflare 2019-07-02 CPU 耗尽事故](https://blog.cloudflare.com/details-of-the-cloudflare-outage-on-july-2-2019/) |
| mall-memory-001 | resource / memory | redis | pending_validation | [Slack 任务队列生产事故](https://slack.engineering/scaling-slacks-job-queue/) |
| mall-network-001 | config / network | mall-portal | pending_validation | [GitHub 2018-10-21 网络分区事故](https://github.blog/news-insights/company-news/oct21-post-incident-analysis/) |
| mall-database-001 | config / database | mysql | pending_validation | [Druid #4078 用户报告](https://github.com/alibaba/druid/issues/4078) |
| mall-cache-001 | config / cache | redis | draft | [Slack 任务队列生产事故](https://slack.engineering/scaling-slacks-job-queue/) |
| mall-mq-001 | architecture / mq | rabbitmq | draft | [Slack 任务队列生产事故](https://slack.engineering/scaling-slacks-job-queue/) |
| mall-disk-001 | resource / disk | rabbitmq | draft | [RabbitMQ #4627 真实复现与维护者讨论](https://github.com/rabbitmq/rabbitmq-server/discussions/4627) |
| mall-threadpool-001 | code / threadpool | mall-portal | draft | [Spring AI #6384 用户报告](https://github.com/spring-projects/spring-ai/issues/6384) |

## 来源与复现差异

### mall-cpu-001

正则规则引起 CPU 耗尽。 仅复现 CPU 争用现象；mall 没有该 WAF 正则，不能声称复现原始代码缺陷。 [Cloudflare 2019-07-02 CPU 耗尽事故](https://blog.cloudflare.com/details-of-the-cloudflare-outage-on-july-2-2019/)

### mall-memory-001

队列积压使 Redis 达到内存上限。 仅以 Pod 内存负载复现容量压力；不是 Redis 数据增长，也不等同 JVM 内存泄漏或原事故根因。 [Slack 任务队列生产事故](https://slack.engineering/scaling-slacks-job-queue/)

### mall-network-001

网络中断触发数据库故障转移和长时间服务异常。 mall 为单数据库；只复现 Pod 网络退化，不复现跨机房故障转移。 [GitHub 2018-10-21 网络分区事故](https://github.blog/news-insights/company-news/oct21-post-incident-analysis/)

### mall-database-001

MySQL 节点失效后报告连接池获取连接超时。 对 mall 单 MySQL Pod 注入丢包；不宣称当前 Druid 存在同一集群连接池缺陷。 [Druid #4078 用户报告](https://github.com/alibaba/druid/issues/4078)

### mall-cache-001

Redis 达到 maxmemory 后拒绝新增工作。 计划使用受限键前缀和可恢复 maxmemory 配置，尚未实现适配；不能把网络丢包当缓存容量问题。 [Slack 任务队列生产事故](https://slack.engineering/scaling-slacks-job-queue/)

### mall-mq-001

数据库争用拖慢消费者并造成队列持续积压。 将同类排队容量问题映射到 mall RabbitMQ；不是 Slack Redis 队列的根因复现，订单业务验收和消费者适配未完成。 [Slack 任务队列生产事故](https://slack.engineering/scaling-slacks-job-queue/)

### mall-disk-001

磁盘告警下发布连接处于 blocked 状态。 计划暂调沙箱 RabbitMQ 水位并恢复原值；不填宿主机磁盘，不承诺复现历史残留连接细节，适配未完成。 [RabbitMQ #4627 真实复现与维护者讨论](https://github.com/rabbitmq/rabbitmq-server/discussions/4627)

### mall-threadpool-001

客户端断开后资源未释放，用户报告 Tomcat 无响应。 该报告未确认且已关闭为 not planned；mall 没有 MCP。当前仅为现象训练候选，必须另做可撤销代码覆盖和请求适配，不宣称存在同一漏洞。 [Spring AI #6384 用户报告](https://github.com/spring-projects/spring-ai/issues/6384)

## 执行边界

CPU、内存、网络延迟、数据库丢包四项已有 Pod ChaosBlade 参数适配，等待服务器验证。注入只接受 sandbox-{session_id} 命名空间及 sandbox-service 从该命名空间列出的、匹配当前模板和 workload 的唯一就绪 Pod。运行时再次查询 content-service；模板关闭、删除或工作负载移除时拒绝启动。CPU/内存强度上限 80%，延迟上限 3000ms，自动撤销时限 120s。不会回退到节点级命令。

其余四项 executor=pending_adapter，状态 draft，尚无实际执行适配，不能验收为可训练，也不能启用。缓存应实现受限测试键与配置恢复；MQ 应控制指定消费链路并验证积压/重复消费；磁盘应模拟沙箱内水位，避免填宿主盘；线程池需受控源码覆盖和请求行为。当前配置不是这些适配器的实现。

Pod 适配也只证明命令边界，不能证明业务故障一定出现。内存负载可能不会导致 Redis maxmemory 写拒绝，数据库丢包不保证复现连接池缺陷。必须用服务器业务证据判定训练价值；不能将注入自动到期当作用户完成修复。当前 metrics/recovery 阈值仅为候选，需按真实 Prometheus 数据单位和标签修改并重新验收；没有指标时不能虚构修复通过。

模板可部署、候选故障入库、正式可训练为三个不同阶段。模拟用户、沙箱支付与 AI 动态调度由平台服务承担，模板保留业务系统及必要适配；真实运行按 [实训验证指南](testing.md)验证，不把代码实现当作已通过验收。

## 后台使用

部署迁移后，在“故障剧本”按 mall 筛选，可查看、编辑来源、模板绑定与所有 JSON 配置。保存任何绑定故障的编辑会清除旧证据、禁用并要求重新验收。无数据库项目的故障不能清空全部模板绑定。

编辑器下方“服务器验收”提交 observed、repaired、revoked、cleanup、validated_at 及当前 template_digest 证据；只有支持的 Pod 配置且来源为具体 incident/issue 才能接受。验收不会同时保存尚未保存的表单编辑。验收通过后另行启用；修改配置后需重复验证。多模板每个绑定分别验收；组合需另行验证，不自动开启任意组合。

## 持久化

目录 JSON 用于初始种子，运行配置由内容域 `content-service` 统一管理。部署时按各服务的迁移顺序初始化并完成历史配置归位，重复种子导入不覆盖管理员编辑；修改或删除故障通过后台操作。后续修改种子 JSON 不会自动覆盖已入库记录。
