# mall 第一阶段服务器加载验收

## 2026-10-29：源码工作区、离线重建与终端目录

完整升级见 [源码工作区与终端目录升级](../../deploy/server/源码工作区与终端目录升级.md)。当前商城 tag 为 `mall-0.2.0`，需重建业务镜像与 web/content-service/sandbox-service，并新建班次。以下早期加载步骤保留为排查参考。

## 2026-10-27：修复 portal 缺失沙箱支付密钥注入

2026-10-19 的 sandbox-service 日志中，班次 6、7 都出现 `mall-portal previous=Error exit=1 restarts=4`，
随后 Helm 等待 5 分钟超时。`signal: killed` 是部署 context 到期终止 Helm 子进程的结果。
这份平台日志没有包含 portal 的 Java 异常堆栈，不能据此断定现场只有一种启动错误。

核对代码发现明确遗漏：`application-sandbox.yml` 要求 `SANDBOX_PAYMENT_SECRET`，
Chart 的 Secret 已生成 `sandbox-payment-key`，但 portal Deployment 未将其注入。
本次补齐 `valueFrom.secretKeyRef`，继续使用每个沙箱独立生成的密钥，不配置固定密码或空值兜底。

升级只需更新包含模板 Chart 的 content-service、sandbox-service 镜像，无需平台数据库迁移。
本次没有修改 mall 业务源码、Dockerfile 或镜像内容；仅因这条修复无需重建商城业务镜像。
默认平台 tag 为 `0.1.0`，其他 tag 与实际部署保持一致：

```bash
(
  set -euo pipefail
  export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
  cd /data/src/quenching
  git pull --ff-only

  IMAGE_TAG=0.1.0 bash deploy/server/images.sh content-service sandbox-service
  for service in content-service sandbox-service; do
    kubectl -n platform rollout restart deployment/"$service"
    kubectl -n platform rollout status deployment/"$service" --timeout=10m
  done
)
```

更新后通过平台新建一次班次。旧班次 6、7 已在日志中记录 TTL 回收，不再对旧环境做现场取证。
若新班次仍启动失败，在五分钟自动回滚前采集对应容器的当前及上一次启动日志：

```bash
# 替换为这一次新建班次的 ID。
SESSION_ID=123
kubectl -n "sandbox-${SESSION_ID}" logs deployment/mall-portal -c mall-portal --tail=200 --timestamps
kubectl -n "sandbox-${SESSION_ID}" logs deployment/mall-portal -c mall-portal --previous --tail=200 --timestamps
```

Helm 部署配置改变会更新模板运行制品指纹。故障仍需按新 `artifact_digest` 验收，不能修改旧验收指纹绕过门禁。
本地检查只证明 Chart 正确引用已生成的密钥；真实商城启动、健康检查与业务链路仍以服务器新班次验收为准。

---

本阶段让平台发现并加载 mall，不包含模拟用户、支付模拟和故障。服务器环境沿用仓库 `deploy/server/README.md` 的单节点 k3s + Docker。
上游版本见 SOURCE.md；目录中的原始 Compose 不是本次部署入口。

## 1. 更新源码与准备存储

在服务器原有 main 分支拉取本次变更。检查 k3s 默认 StorageClass：

```bash
cd /data/src/quenching
git pull --ff-only
kubectl get storageclass
```

Chart 默认使用集群默认 StorageClass。该 class 必须存在、支持 RWO PVC，且 `reclaimPolicy=Delete`，才能在沙箱销毁后清理磁盘数据。
若指定其他 class，在构建平台镜像前修改 `templates/mall/helm/values.yaml` 的 `persistence.storageClass`。
不要改成 Retain 或宿主机共享目录。`persistence.enabled=false` 仅适合一次性加载检查：Pod 重建会丢数据，不作为后续实训的默认方式。

## 2. 构建并导入模板镜像

使用具有 Docker 和 k3s 操作权限的账号：

```bash
bash templates/mall/scripts/build-images.sh
```

脚本构建 portal、H5 前台与带初始化 SQL 的 MySQL 三个镜像，再拉取并导入 Redis、RabbitMQ、MongoDB、busybox。
Docker 与 k3s containerd 的镜像库独立，不能跳过导入。多节点集群需在每个可能调度的节点导入，或自行配置可访问镜像仓库。
构建阶段需要访问 Maven、npm 和镜像源，运行时不下载构建依赖。前端使用上游 package-lock.json 的 `npm ci`。
默认模板 tag 是 `mall-0.2.0`。如果设置 `MALL_IMAGE_TAG`，须同步修改 Chart 的 `global.image.tag` 后再构建平台镜像；平台不会自动根据环境变量替换模板 values。
需要镜像仓库时可以编辑 `global.image.registry`、各服务 `image.repository/tag`、`pullSecrets`。覆盖 registry 会影响公共依赖镜像，也必须同步镜像到该仓库。
本阶段端口固定，由 schema 校验；不要仅修改端口而不调整配置与镜像。

## 3. 更新平台镜像与服务

清单与 Chart 打包在 content-service、sandbox-service 镜像内，单纯 git pull 或重启旧镜像不会更新模板。
web 也有必要改动：创建沙箱的请求允许等待6分钟，对应后端最长5分钟的同步部署，普通请求仍保留15秒超时。

```bash
# IMAGE_TAG 与当前平台部署的 global.imageTag 保持一致。
IMAGE_TAG=0.1.0 bash deploy/server/images.sh content-service sandbox-service web
kubectl -n platform rollout restart deployment/content-service deployment/sandbox-service deployment/web
kubectl -n platform rollout status deployment/content-service
kubectl -n platform rollout status deployment/sandbox-service
kubectl -n platform rollout status deployment/web
```

若使用不同平台 tag，请按已有平台发布流程更新对应 image。外层另有反向代理时，创建请求的响应等待时间也需覆盖部署时间；仓库自带 Web nginx 已允许该等待。
不需要为模板创建数据库项目记录或执行新的平台数据库迁移。

## 4. 验证平台发现并创建沙箱

刷新平台模板列表，应出现“mall 电商交易实训”（展示名称来自 `catalog.yaml`，不代表本节验收已通过）。也可用当前登录 token 检查：

```bash
curl -fsS -H "Authorization: Bearer ${TOKEN}" "${GATEWAY}/api/v1/templates"
```

响应应包含 `slug: mall`、六个服务及 `chart: helm`。
从模板入口创建班次并加载，等待沙箱 ready，记录会话 ID。平台创建 `sandbox-<会话ID>`、资源配额与网络隔离，再安装 mall release。
未声明 behavior_flow，所以不会创建行为 runner。平台桌面应显示六个业务/中间件 Pod；这不证明已经具备故障训练能力。

## 5. 检查实际加载

使用刚才的会话 ID，例如：

```bash
bash templates/mall/scripts/verify-loading.sh sandbox-123
```

脚本检查隔离资源、Deployment 就绪、前台健康、portal 健康、首页业务 API，以及前台和后端支付路径返回503。
再通过端口转发查看真实 H5 页面：

```bash
kubectl -n sandbox-123 port-forward service/frontend 18080:80
```

在执行端口转发的服务器本机访问 `http://127.0.0.1:18080/`；远程查看时可用 SSH 本地转发。
前台 API 与页面同源，支付不可用。上游商品图片中仍可能存在外链，本阶段不将外链图片可见性算作完整实训通过，后续须适配本地资源。
本轮不提供平台注册完成交易的模拟行为，不将单次首页加载当成完整交易验收。

## 6. 销毁与记录

通过平台结束/放弃验收班次，确认对应 namespace 被回收。检查相关 PVC/PV 已按 Delete 策略清理，再创建一个新班次验证初始数据重建。
失败排查：

```bash
kubectl -n sandbox-123 get pods,pvc,events
kubectl -n sandbox-123 describe pod <异常Pod名>
kubectl -n sandbox-123 logs deployment/mysql
kubectl -n sandbox-123 logs deployment/mall-portal --all-containers
kubectl -n platform logs deployment/sandbox-service
```

首次创建失败时查看平台日志；Provision 会卸载 release，原沙箱记录会标记失败，不应把正在运行的其他班次删除来重试。
把实际构建、启动、访问、销毁结果和证据填写到 ACCEPTANCE.md。未完成项目保持待验证。


## 训练大脑验收（2026-10-05）

详见 [实训验证指南](../../docs/testing.md)。本地检查不代表真实模型、集群或交易验收通过；候选故障保持验收门禁。
