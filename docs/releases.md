# 版本与发布约定

淬火以整个平台作为一个发布单元，继续在 `main` 上开发，用 Git Tag 固定每次对外发布的源码。模板项目和数据库迁移保留各自的版本规则。

## 版本编号

首个公开候选版本为 `v0.1.0-alpha.1`，随后在同一阶段递增为 `v0.1.0-alpha.2`、`v0.1.0-alpha.3`。真实模型及首个完整实训闭环验证后，根据实际质量进入 `v0.1.0-beta.1`；完成该轮验收后发布 `v0.1.0`。当前版本完成元数据准备不代表 GitHub Release 已发布。

版本标签本身不代表某项故障已验收，也不把 `0.1.0` 定义为生产环境承诺。具体能力、验证范围和已知限制必须写进 Release Notes。

稳定发布后的兼容修复递增修订号，如 `0.1.1`；新增一轮功能递增次版本号，如 `0.2.0`。`0.x` 阶段发生不兼容修改时，明确写出影响和迁移方法。编号依据 [语义化版本规范](https://semver.org/lang/zh-CN/)。

日常提交先累积在 [CHANGELOG 的未发布部分](../CHANGELOG.md)，形成可说明、可验证的一组变化后再发布。已经发布的 Tag 和镜像标签固定不变，修改通过新版本交付；不反复覆盖同一个 `0.1.0` 镜像。

## 一次发布需要对应哪些版本

| 对象 | 首版示例 | 规则 |
| --- | --- | --- |
| Git Tag / Release | `v0.1.0-alpha.1` | 固定到一个已提交的 `main` commit，Alpha/Beta 标记为预发布 |
| 平台镜像 | `quenching/<服务>:0.1.0-alpha.1` | 去掉 Tag 的 `v` 前缀；11 个平台镜像从同一源码版本构建 |
| 平台 Helm Chart | `version: 0.1.0-alpha.1`、`appVersion: "0.1.0-alpha.1"` | 同步本次平台发布版本，镜像选择仍由 `global.imageTag` 控制 |
| 项目元数据 | `0.1.0-alpha.1` | 同步根目录及 web/admin/shared 的 package.json、AI 工程版本与 FastAPI 版本；Python 元数据按 PEP 440 写为 `0.1.0a1` |
| mall 模板镜像 | 当前 `mall-0.2.0` | 独立版本，平台发版时不自动改名；模板或适配变化时单独更新并重新验收 |
| 数据库迁移 | 各服务迁移文件序号 | 不跟随平台版本重置或重命名，已应用的迁移不修改 |

发布前同步 `deploy/helm/values.yaml`、`deploy/server/values-server.yaml` 的默认镜像标签，以及 `images.sh`、`migrate.sh`、`deploy.sh`、`upgrade-observability.sh` 和 `manifests/20-frontend.yaml` 中的默认值。当前已对齐到 `0.1.0-alpha.1`。模板镜像、第三方依赖和 API 契约中的独立版本不一起替换。

根 `package.json` 的版本是发布校验基准，运行 `python scripts/release/check_version.py` 检查各处是否一致；GitHub CI 也执行该检查。

API 路径中的 `/api/v1` 是接口契约版本，不能因每次平台发版而改名。

Python 工程版本采用 [Python 版本规范](https://packaging.python.org/en/latest/specifications/version-specifiers/#pre-releases)的 `a`、`b`、`rc` 表示预发布；这是同一平台发布在 Python 元数据中的对应写法。

## 每次发布的步骤

1. 汇总本轮改动，更新 CHANGELOG，逐条写明本地检查、真实环境验证和未验证项。
2. 同步上表中的平台版本，确认模板版本与故障验收指纹仍匹配；版本修改也需要提交到 `main`。
3. 执行相关测试、构建和 Helm 渲染，核对发布前的平台 Apache-2.0 许可证、上游声明及敏感信息检查。
4. 从干净工作区导出已提交源码到公开仓库。GitHub 首次提交仅含当前源码，不带 Gitee 原历史；后续只在确认公开分支基线后更新。核对远端最新提交后，为公开提交创建附注 Tag。
5. 以该 Tag 的源码构建镜像，记录 commit、镜像摘要、Chart 版本、模板镜像和各服务迁移终点。当前脚本构建并导入本机 k3s，不会自动把镜像推送到公共镜像仓库。
6. 发布 Release Notes，Alpha/Beta 勾选预发布；部署时使用对应版本的说明，验收通过后再更新在线体验环境。

GitHub 与 Gitee 的首次历史不同，commit SHA 也不同。原 Gitee 继续保存完整开发历史；GitHub 保存从首个公开快照开始的 `main`。发布时记录源提交、公开提交和相同的源码 tree，不能将同版本号误认为两个仓库的 commit SHA 相同。导出及后续同步见 [公开快照工具](../scripts/release/README.md)；公开分支有新贡献时先审阅导入，禁止强推覆盖。

Tag 命令示例（在开发仓库根目录执行，显式操作工具生成的公开暂存仓库；先确认源码与检查结果）：

```bash
git -C output/github-public status --short
git -C output/github-public rev-parse HEAD
git -C output/github-public tag -a v0.1.0-alpha.1 -m "Quenching 0.1.0-alpha.1"
git -C output/github-public push origin main:main
git -C output/github-public push origin v0.1.0-alpha.1
```

上述 Tag 与推送只作用于 `output/github-public`，不对保留原历史的 Gitee 仓库执行。Release 发布状态及 GitHub Actions 结果按实际远端记录确认。

敏感信息检查需要覆盖当前源码与 Git 历史，并区分真实凭据、开发默认值和上游示例。平台开发配置含固定的 DB/MQ 口令及 JWT 示例，部署时需要替换；mall 上游快照含演示账号、示例 SQL 和历史 Postman 令牌，保留其来源及许可，不将这些数据当作生产配置或真实业务证据。已解码令牌中的过期时间不能代替实际失效验证。

部署脚本的离线回归使用临时目录和模拟命令，不访问真实集群：

```bash
bash scripts/qa/deploy-version.sh
bash scripts/qa/migration-order.sh
```

## 部署时使用同一个镜像标签

在对应发布版本的源码目录中构建全部平台镜像：

```bash
export IMAGE_TAG=0.1.0-alpha.1
bash deploy/server/images.sh
```

首次安装完成 [服务器前置步骤](../deploy/server/README.md)和对应版本的迁移后，可以使用以下命令安装平台并暴露前端。`deploy.sh` 会把同一个标签传给 Helm 并渲染前端镜像：

```bash
IMAGE_TAG=0.1.0-alpha.1 bash deploy/server/deploy.sh
```

`deploy.sh` 默认生成密钥，不能把它当成保留现有密钥的通用升级入口。现有环境按该版本的升级说明处理；有迁移时先按要求停写，再将同一个 `IMAGE_TAG` 传给 `migrate.sh`。Helm 设置 `global.imageTag`，独立部署的 web/admin 也必须更新到同标签，不能只改后端。日志采集器由 Helm 选择镜像，商城镜像仍读取模板自己的配置。多节点需要分发镜像，或使用各节点可拉取的镜像仓库。

每版说明需记录升级前置条件、是否停训停写、迁移顺序、配置变化和恢复方法。代码版本回退不能自动回退数据库结构或班次数据；不要用旧镜像直接覆盖已经执行不兼容迁移的数据库。

### 从近期 `0.1.0` 开发部署切换到首个 Alpha 标签

本轮发布准备没有新增数据库迁移；更旧环境仍先按 [近期升级说明](../deploy/server/近期升级说明.md)补齐前序迁移与权限配置。停止接纳新实训并结束、清理已有班次后，在原服务器执行。下面保留现有 Helm 密钥和覆盖值，重新构建全部平台镜像并更新前后端引用；商城仍使用模板自己的镜像版本。

```bash
(
  set -euo pipefail
  export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
  cd /data/src/quenching
  git pull --ff-only
  export IMAGE_TAG=0.1.0-alpha.1
  bash deploy/server/images.sh
  helm upgrade quenching ./deploy/helm -n platform \
    --reset-then-reuse-values --set-string global.imageTag="$IMAGE_TAG" \
    --wait --timeout=10m
  for frontend in web admin; do
    kubectl -n platform set image deployment/"$frontend" "$frontend=quenching/$frontend:$IMAGE_TAG"
  done
  for service in api-gateway content-service user-service training-service review-service fault-engine sandbox-service ai-service web admin; do
    kubectl -n platform rollout status deployment/"$service" --timeout=10m
  done
  kubectl -n platform rollout status daemonset/quenching-log-agent --timeout=10m
)
```

自定义镜像仓库的环境需相应替换前端 `quenching/` 路径。当前镜像脚本只导入执行节点，多节点按运维手册分发同版本镜像后再升级。

## Release Notes 模板

```text
版本 / 发布日期 / 源码 commit：
新增与修复：
已验证范围：
已知问题与未验证项：
部署与升级步骤（镜像、Helm、配置、数据库迁移）：
兼容的模板镜像与已验收故障：
恢复方式与限制：
```
