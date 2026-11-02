# 淬火平台 · 单机部署手册（火山引擎）

适用环境：单台云主机 8核16G，系统盘 40G + 数据盘 100G；操作系统 Debian/Ubuntu 系（root 登录）。
交付形态：公网 IP + HTTP 自测；域名、HTTPS、备案后置。

公开版本的编号、镜像标签和升级记录按 [版本与发布约定](../../docs/releases.md)管理。当前候选默认标签为 `0.1.0-alpha.1`，构建、迁移、Helm 与前端必须使用同一平台镜像标签。已有 `0.1.0` 环境切换新标签时需要构建全部平台镜像并更新镜像引用，不能只重启旧标签的服务。

| 入口 | 地址 |
| --- | --- |
| 用户端 web | `http://<公网IP>:30080` |
| 管理端 admin | `http://<公网IP>:30081` |
| API 网关 | `http://<公网IP>:30082` |
| Prometheus | 集群内 `http://prometheus-server:9090`（如需公网见文末） |

> 已部署服务器更新近期版本，先看 [近期升级说明](近期升级说明.md)：需要新增迁移、Helm/RBAC 更新和旧班次清理，不能只重启镜像。

2026-10-19 的工作台界面更新见 [工作台升级与验收](工作台升级与验收.md)；已完成前序升级时仅需重建并重启 web。
模板首页封面和项目介绍更新见 [模板展示升级与验收](模板展示升级与验收.md)，包含图片端点，需要网关与 Helm 路由一起更新。

项目大厅搜索、领域筛选和首屏布局压缩见 [项目大厅布局升级](项目大厅布局升级.md)，仅需更新 web 镜像。

## 0. 前置准备

火山引擎控制台确认：

- 数据盘已 **attach** 到实例（控制台"已挂载"仅表示这一步，OS 内仍需分区挂载，由 `01-bootstrap.sh` 完成）。
- 安全组入方向放行 TCP：`30080`、`30081`、`30082`（如要 SSH 还需 22）。

## 1. 初始化环境

将代码放到 `/data/src/quenching`（数据盘），然后：

```bash
cd /data/src/quenching/deploy/server
bash 01-bootstrap.sh
```

脚本幂等，完成：数据盘分区/ext4/挂载 `/data`（含 `/etc/fstab`）、k3s（数据目录 `/data/k3s`）、containerd 镜像加速（DaoCloud）、helm。

> 若机器上 k3s 已手工装好（当前现场即如此），重跑只会补齐配置并重启 k3s，不会重装。

## 2. 安装 Docker（构建镜像用）

k3s 自带 containerd 用于运行，但 `images.sh` 用 Docker 构建。如宿主机还没有 Docker：

```bash
curl -fsSL https://get.docker.com | sh
systemctl enable --now docker
```

构建产物会在脚本末尾通过 `k3s ctr images import` 导入 k3s，因此 Docker 与 k3s 各用各的存储也没关系。

## 3. 部署中间件

```bash
cd /data/src/quenching
kubectl apply -f deploy/server/manifests/00-namespace.yaml
kubectl apply -f deploy/server/manifests/01-rbac.yaml
# Install shared priorities first; ownership matches the later Helm release.
helm template quenching ./deploy/helm -n platform --show-only templates/resource-priorities.yaml | kubectl apply -f -
kubectl apply -f deploy/server/manifests/10-middleware.yaml

kubectl -n platform rollout status deploy/prometheus-server
kubectl -n platform rollout status statefulset/postgresql
kubectl -n platform get pods
```

全部 Running 后再继续。PostgreSQL 首次拉起会初始化库/用户 `quenching`。

## 4. 构建并导入镜像

```bash
cd /data/src/quenching/deploy/server
bash images.sh
```

构建 11 个镜像（7 Go + ai-service + web + admin + log-agent），默认 tag `0.1.0-alpha.1`，随后自动导入 k3s containerd。通过 `IMAGE_TAG` 显式指定其他标签；log-agent 构建时会校验采集配置并执行转换回归。
只重建部分服务：`bash images.sh web admin`。

## 5. 数据库迁移

```bash
cd /data/src/quenching/deploy/server
bash migrate.sh
```

六个服务共库、各自独立迁移表（`schema_migrations_content_service`、`schema_migrations_fault_engine` 等，规则同仓库根 Makefile）。脚本按 fault-engine → training-service → user-service → content-service → sandbox-service → review-service 串行创建并等待迁移 Job，历史源域完成后才复制到新属主；首次安装也不能并行 apply 全部 render 结果。重跑：`bash migrate.sh clean && bash migrate.sh`。存量升级先按近期升级说明停写、备份。

## 6. 安装平台服务

先生成密钥（不要沿用文件里的占位值）：

```bash
JWT_SECRET="$(openssl rand -hex 32)"
INTERNAL_TOKEN="$(openssl rand -hex 32)"

cd /data/src/quenching
bash deploy/server/prepare-logs.sh
helm install quenching ./deploy/helm \
  -n platform \
  -f deploy/server/values-server.yaml \
  --set-string logging.persistence.hostPath="$PWD/logs" \
  --set global.jwtSecret="${JWT_SECRET}" \
  --set global.internalToken="${INTERNAL_TOKEN}" \
  --set observability.wsGatewayApiKey="${INTERNAL_TOKEN}" \
  --wait
```

`platform` 命名空间与标签是沙箱 NetworkPolicy 的硬约束，不能改名。

沙箱终端与文件操作按班次自动授权，结束时撤销，环境销毁后下一班次从模板基线创建。旧部署遇到 `pods/exec` Forbidden 的升级步骤见 [终端连接排查](终端连接排查.md)。

源码编辑、离线构建和终端/文件目录同步见 [源码工作区与终端目录升级](源码工作区与终端目录升级.md)。本次需商城 `mall-0.2.0` 新镜像和新班次。

结束训练后立即离开工作台、后台回收和复盘见 [实训结束后台处理升级](实训结束后台处理升级.md)，需同时更新 web 与 training-service。

手机号注册验证码报错及密码显示／隐藏修复见 [注册与密码可见性升级](注册与密码可见性升级.md)，需更新 user-service、web、admin，并下发 Helm 注册策略。

平台日志同时保留 `kubectl logs`，并写入仓库根目录的 `logs/<服务名>/<YYYY-MM-DD>/`。八个后端与浏览器异常按大小轮转；NGINX、PostgreSQL、Redis、RabbitMQ、Prometheus 由节点 log-agent 按 Pod/小时分文件采集，保留 30 天。浏览器异常分别落到 web-browser/admin-browser；服务器日志按配置的北京时间组织，前端页面按用户浏览器当地时区显示，日志详情附带事件的 UTC 偏移。数据库仍使用 TIMESTAMPTZ 保存实际时刻。详细升级、目录结构及校验见 [日志与部署诊断](日志与部署诊断.md)。

## 7. 暴露网关与前端

```bash
kubectl apply -f deploy/server/manifests/15-gateway.yaml
kubectl apply -f deploy/server/manifests/20-frontend.yaml
kubectl -n platform get svc
```

看到三个 LoadBalancer 的 EXTERNAL-IP 为节点 IP（单节点 ServiceLB 行为）即可。

## 8. 默认账号

来自 user-service 种子迁移：

| 角色 | 账号（手机号） | 密码 |
| --- | --- | --- |
| 超级管理员 | `13800000000` | `Admin@123` |
| 演示用户 | `1380000001` | `User@123` |

上线对公网开放后请立即改密。

## 9. 验证清单

1. 浏览器打开 `http://<IP>:30080`，用演示用户登录，页面与接口正常。
2. 打开 `http://<IP>:30081`，用管理员登录，项目/沙箱/缺陷等列表可加载。
3. 选择真实开源 `mall` 模板创建沙箱，按 [商城加载验收](../../templates/mall/SERVER_DEPLOY.md) 检查启动、业务接口和隔离。
4. 后台项目管理应显示官方 mall 模板；“查看故障”进入该模板的故障列表，“配置坑点”进入对应故障的坑点列表。
5. 按 [实训验证指南](../../docs/testing.md) 验证真实注册、交易、沙箱支付、助手、故障及整班次复盘；候选故障逐项通过服务器验收后才可启用。
6. 真实 AI 功能需要配置 `ai.llmApiKey`，模型统一为 `qwen3.8-flash`；服务健康检查通过后还需验证真实模型调用。

## 资源与容量

平台与实训共用资源池，按模板资源计划和节点可用容量准入；容量不足会排队，不再按固定每沙箱 2C4G 推算并发数。实际并发取决于平台占用、模板和节点资源。
查看占用：`kubectl -n platform top pod`、`df -h /data`。

## 常见问题

- **Pod ImagePullBackOff**：多半是镜像没导入 k3s。`k3s ctr images ls | grep quenching` 检查；重跑 `images.sh` 的导入段。
- **公网打不开但 Pod 正常**：先在服务器 `curl http://localhost:30080`，通就是安全组没放行。
- **外网镜像拉取超时**：加速配置在 `/etc/rancher/k3s/registries.yaml`（DaoCloud）。仍失败可换 `https://dockerproxy.net`、`https://docker.1panel.live` 等端点后 `systemctl restart k3s`。
- **迁移 Job 失败**：`kubectl -n platform logs job/migrate-<服务> -c migrate`；修复后 `migrate.sh clean` 重跑。
- **想看 Prometheus 界面**：临时 `kubectl -n platform port-forward svc/prometheus-server 9090:9090`；不建议直接对公网开。
- **清理自带 Traefik**：当前安装未禁用成功，闲置约 200MB 内存，不影响本方案；需要清理时删除 `kube-system` 中 traefik/svclb-traefik 及两个 helm-install Job 的相关资源即可。

## 统一资源池与多机扩展

平台与实训环境共用现有 Kubernetes 资源池；后台入口为“沙箱管理 → 资源池”。资源行为验证见 [实训验证指南](../../docs/testing.md)，新增工作节点和运维操作见 [服务器运维手册](服务器运维手册.md)。当前本地导入镜像需分发到新增节点；local-path卷不会自动跨机器迁移。

平台整体与每次实训分别汇总占用、服务明细折叠展示的更新见 [资源池按次分配汇总升级](资源池按次分配汇总升级.md)，需同步更新 sandbox-service 与 admin。
