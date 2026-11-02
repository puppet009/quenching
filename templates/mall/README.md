# mall 电商模板（接入验证）

状态：第一阶段加载配置已准备，待服务器构建与运行验收。`template.yaml` 已启用，平台服务更新后可发现并尝试部署。
仅用于负责人加载验收环境，不代表正式实训准入通过；尚无模拟用户、支付模拟或故障。

第一候选采用真实开源项目 [macrozheng/mall](https://github.com/macrozheng/mall)，
配套商城前端为 [macrozheng/mall-app-web](https://github.com/macrozheng/mall-app-web)。
上游快照保留原文，部署适配放在独立目录；两个固定版本尚未完成服务器配套运行验证。

## 目录与版本

- `upstream/mall/`：后端及上游自带部署资料、数据库 SQL、许可证。
- `upstream/mall-app-web/`：商城前端及许可证。
- `upstream/*.source.json`：上游 URL、固定 commit、导入时间及导出 ZIP 的 SHA-256。
- `SOURCE.md`：来源和适配差异。
- `ACCEPTANCE.md`：服务器验收清单。
- `template.yaml`、`helm/`：扫描声明及六个服务的部署配置。
- `catalog.yaml`：用户展示资料，介绍电商项目、业务场景、训练目标及组件职责；不代表验收通过。
- `catalog-assets/`：单张官方首页封面与内部来源记录，不修改上游快照，不参与故障验收指纹。
- `docker/`、`adaptation/`、`scripts/`：构建、沙箱配置、数据初始化与检查。
- `SERVER_DEPLOY.md`：本阶段服务器部署步骤。

后端固定源码声明 JDK 17、Spring Boot 3.5.14，采用 Maven 多模块构建。
上游前端启动方式以 `upstream/mall-app-web/README.md` 为准。
具体依赖和资源占用尚未实测，不把整个上游演示部署视为符合淬火资源预算的最小部署。

## 部署入口

淬火接入使用 `SERVER_DEPLOY.md`，不要直接执行上游完整 Compose。
本阶段部署 frontend、mall-portal、MySQL、Redis、RabbitMQ、MongoDB。
配置限额合计 2300m CPU、3712Mi 内存（包括有效 init 容器限额），实际启动与源码构建占用仍待服务器实测。
数据库首次启动导入固定 SQL 并清除演示客户、订单与相关记录。四个状态服务及两个源码工作区使用班次独立 PVC，要求 StorageClass 回收策略为 Delete。

源码镜像版本 `mall-0.2.0`：前端 `/workspace/mall-app-web`、Java `/workspace/mall`，使用 `training-rebuild` 离线构建并重新运行。工作区、可编辑配置和发布结果在同班次容器重启后保留，结束后随沙箱回收。升级与真实验收见 [源码工作区与终端目录升级](../../deploy/server/源码工作区与终端目录升级.md)。
前台 API 为同源相对路径，不调用上游演示 API；支付在前台与 sandbox 后端两层返回 503。
不部署后台管理、搜索或外部模拟服务，不启动行为 runner。

以下上游资料仅供参考：

- 后端：`upstream/mall/README.md`。
- 上游 Docker 参考：`upstream/mall/document/reference/docker.md`。
- 上游 Compose：`upstream/mall/document/docker/docker-compose-env.yml` 与 `docker-compose-app.yml`。
- 数据库初始化：`upstream/mall/document/sql/mall.sql`。

以上文件是未修改的上游资料，并非完成适配的淬火部署包。上游示例地址、默认凭据、支付配置与初始化账号需要服务器部署时按沙箱要求审查和替换。
本轮不安装本地运行环境，不执行部署；服务器部署与验收由项目负责人完成。

## 接入目标

按上级 `templates/README.md` 准入规范，后续完成沙箱部署、业务数据初始化、
模拟用户注册与交易、支付等外部服务隔离模拟、故障绑定、业务恢复验证和班次重置。
同一班次保留学员修改，结束先保存复盘证据再清空运行状态。
故障定义与项目绑定由后台统一管理，当前没有把未经验证的故障候选作为已支持能力发布。

当前清单允许负责人在验收环境加载；只有完整准入验收有证据且平台实训适配完成，才能正式向学员开放。


## 训练大脑验收（2026-10-05）

详见 [实训验证指南](../../docs/testing.md)。本地检查不代表真实模型、集群或交易验收通过；候选故障保持验收门禁。
