# 源码来源与适配记录

导入日期：2026-10-02。来源均为 GitHub 上游公开仓库。

| 项目 | 上游仓库 | 固定 commit | 上游许可证 |
| --- | --- | --- | --- |
| mall 后端 | https://github.com/macrozheng/mall | `dcaa93b3150352e5044708d7211b5bed0af4509f` | Apache-2.0，见 `upstream/mall/LICENSE` |
| mall 商城前端 | https://github.com/macrozheng/mall-app-web | `b1eb3cde1d74fbf62e72195bafd9322c8149f4c8` | Apache-2.0，见 `upstream/mall-app-web/LICENSE.txt` |

## 导入方法

从各上游默认分支浅克隆，取得完整 commit，再通过 `git archive --format=zip HEAD` 导出该版本跟踪文件。
源码快照不包含嵌套 `.git`、本地依赖或构建产物；保留上游许可证及文件原文。
两个仓库版本分别固定，尚未验证前后端兼容性。
机器可读来源与导出文件校验和记录在 `upstream/mall.source.json` 和 `upstream/mall-app-web.source.json`。

Windows TAR 解包曾在中文文件名处失败，最终使用 ZIP 重新导入，并按导出 ZIP 对全部文件进行内容核验。

## 平台适配差异

2026-10-29 增加可编辑源码工作区：前端镜像保留准确构建版本的 Vue 源码、Node/npm 与依赖缓存；后端镜像提供准确构建版本（含上述平台适配类）的 Java 源码、JDK17/Maven 与离线仓库。原始 upstream 快照继续保留不变。`frontend/portal-entrypoint.sh` 为每班次独立工作区初始化源码与运行文件；`frontend/portal-rebuild.sh` 离线构建并发布，失败时保留已发布结果。工作区与发布配置保存在本班次 PVC，结束后回收。使用说明见 `adaptation/TRAINING.md`，真实镜像构建和运行仍待服务器验收。

上游快照未修改。当前加载阶段的差异如下：

- `docker/portal.Dockerfile`：JDK17/Maven 构建 portal 及依赖，关闭上游 package 阶段的远程 Docker 插件。
- `adaptation/application-sandbox.yml`：业务依赖指向本沙箱，凭据从 Secret 注入，限制连接池并开启健康检查，支付宝网关指向禁用的本机地址。
- `adaptation/LoadingPaymentPaths.java`、`LoadingStagePaymentFilter.java`：构建时添加 sandbox profile 专用支付拦截，不更改上游快照；阻止 SDK 支付和原始直接标记支付成功的入口。
- `adaptation/logback-sandbox.xml`：使用 stdout，避免依赖上游日志分析服务。
- `docker/frontend.Dockerfile` 与 `adaptation/nginx.conf`：H5 API 指向同源沙箱后端，前台支付路径返回503；不改变上游页面业务源码。
- `docker/mysql.Dockerfile` 与 `adaptation/20-reset-customers.sql`：导入固定上游 SQL 后清空演示客户和订单记录、释放演示库存锁定，保留商品等初始化数据。
- `helm/`：六个服务、内部地址、Secret、健康探针、PVC 和资源约束；命名空间配额及网络隔离继续由平台负责。

新增支付限制只用于加载阶段，不是故障注入，不宣称已有支付模拟服务。
后续适配应记录涉及文件、目的、与上游行为的差异，以及验证结果；不把主动植入的训练缺陷称作上游已有缺陷。
