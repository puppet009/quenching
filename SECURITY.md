# 安全反馈

淬火目前处于 Alpha 阶段，仅维护最新的 `main`，旧版 Tag 暂不单独维护。

发现可能影响淬火安全的漏洞，请通过 [GitHub 私密漏洞报告](https://github.com/puppet009/quenching/security/advisories/new) 联系维护者。请尽量提供受影响的版本或 commit、复现步骤、影响范围及脱敏证据；暂不清楚的内容可以注明“未知”。

请勿在公开 Issue 或 PR 中披露未修复漏洞的可利用细节，也不要提交真实凭据、客户数据或未获准公开的内部资料。维护者会在私密报告中沟通核查、修复与公开说明。

普通 Bug、功能建议和故障经验分享请使用 [贡献入口](CONTRIBUTING.md)。

## 已核查的上游标识符

2026-11-03 核查 GitHub secret scanning 告警 #1：`templates/mall/upstream/mall-app-web/src/manifest.json` 中的 `mp-weixin.appid` 与[固定上游版本](https://github.com/macrozheng/mall-app-web/blob/b1eb3cde1d74fbf62e72195bafd9322c8149f4c8/src/manifest.json)相同。该字段是微信小程序的客户端应用标识，不是服务端 `AppSecret`；字段含义见 [uni-app 配置文档](https://uniapp.dcloud.net.cn/collocation/manifest#mp-weixin)。

当前商城镜像和工作区重建均使用 `build:h5`，未接入微信鉴权；在已跟踪源码中未检出配套 `AppSecret`。该标识仍随上游源码进入镜像的可编辑源码种子，本次判断不代表其已失效、已吊销或已从所有制品移除。

本条按非凭据误报记录，保留原始上游快照及模板指纹。此结论仅适用于上述固定版本、字段和当前 H5 使用范围，不排除整个上游目录的扫描。发现真正的密钥、访问令牌或使用范围变化时，需要重新核查；密钥扫描及推送保护继续开启。
