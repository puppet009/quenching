# 从当前源码建立 GitHub 公开历史

私有/Gitee 仓库继续在 `main` 开发并保留原历史。此工具只读取当前已提交的 `HEAD` 文件树，在忽略目录 `output/github-public/` 建立独立的公开 Git 仓库。首次公开提交没有父提交，后续公开提交只连接公开历史；不会复制私有 `.git`、远端地址、提交说明或旧版本文件。

需要 Python 3.10+ 和 Git。以下命令在私有源码仓库根目录执行。工具不会推送、创建 Tag、删除目录或修改源码仓库的 Git 元数据。

## 首次准备

先完成源码检查并提交到 `main`。已跟踪文件必须没有未提交修改；未跟踪和忽略文件不会导出。工具还会拒绝当前提交中的 `docs/internal/`、`docs/superpowers/`、日志、依赖和本地产物目录，以及未解析的 Git LFS 指针和子模块。它不替代凭据、许可证或发布内容审查。

GitHub 仓库应为空，不预建 README、许可证或 `.gitignore`。正常准备会先以只读方式检查远端没有任何引用：

```powershell
python scripts/release/public_snapshot.py prepare --remote https://github.com/puppet009/quenching.git --author-name puppet009 --author-email 101040093+puppet009@users.noreply.github.com --message "Quenching 0.1.0-alpha.1"
```

仓库尚未创建或暂时无法认证时，可在相同命令上加 `--offline`，生成本地候选。此模式不检查远端是否存在或为空，输出 `remote_status: not-checked`；发布前仍需确认目标账户和空仓库。普通推送会拒绝覆盖无共同历史的已有 `main`，不要加 `--force`。

输出目录已存在时，工具拒绝覆盖。可用 `--staging output/另一个未使用目录` 准备另一个候选；后续命令必须传入相同参数。不要重新准备已发布的仓库来代替增量同步。

## 审查与首次推送

```powershell
git -C output/github-public log --oneline --all
git -C output/github-public ls-tree -r --name-only HEAD
git rev-parse "HEAD^{tree}"
git -C output/github-public rev-parse "HEAD^{tree}"
git -C output/github-public push origin main:main
python scripts/release/public_snapshot.py status
```

两份源码树哈希应一致。首次公开历史只有一个 `main` 根提交。公开 commit 与私有 commit 不同；提交映射和两个树哈希记录在本地 `output/github-public/.git/quenching-public.json` 及同目录的 `quenching-public-audit/*.json` 中，不进入公开文件树。保留该目录及审计文件，后续同步需要它们。

文件内容直接读取 Git blob，不应用 `export-ignore` 或 `export-subst`，保留可执行位和符号链接的 Git 模式。Windows 检出符号链接时使用普通链接文本文件，公开 Git 树仍保留 `120000` 模式。敏感信息检查应针对导出的公开文件及公开历史进行。

## 后续同步

继续在原私有/Gitee `main` 开发、验证并提交，然后执行：

```powershell
python scripts/release/public_snapshot.py status
python scripts/release/public_snapshot.py export --message "Describe this public update"
git -C output/github-public log -2 --oneline
git -C output/github-public diff HEAD~1 HEAD --stat
git -C output/github-public push origin main:main
python scripts/release/public_snapshot.py status
```

`export` 必须在线确认 `origin/main` 等于上次审计的公开 commit，然后在该 commit 上增加一个公开提交。若源码树没有变化，只更新必要的本地审计，不制造空提交。首次候选必须推送后才能执行增量导出。每次导出后先完成推送，再开始下一次导出。

工具拒绝公开远端漂移、未知本地提交、脏的公开工作区、修改过的 `origin`、重写的私有历史或不再位于 `main` 的仓库。公开暂存工作区中的忽略文件也会使操作停止，避免后续检出覆盖本地文件；在原源码仓库中，忽略文件仍可正常保留。`status` 发现远端漂移返回退出码 1。网络或认证失败也会停止，不会猜测远端为空。推送时若再次发生竞争，普通 Git 推送会拒绝覆盖。

## 接受 GitHub 外部贡献

公开 `main` 合入 PR 后，`export` 会停止。先审查贡献，将其内容导入原私有/Gitee `main`，完成测试并提交；原仓库仍保留自己的历史，不合并不相关的公开历史。

只有私有当前源码树与远端公开 `main` **完整一致**时，才执行：

```powershell
python scripts/release/public_snapshot.py adopt
python scripts/release/public_snapshot.py status
```

`adopt` 仅在独立公开仓库获取远端 `main`，确认其延续已审计的公开历史，并核对整个树与当前私有 `HEAD` 相等，随后更新本地公开 `main` 并记录映射。不匹配时保持本地公开 `main` 和审计不变；获取的公开对象可能留在公开 `.git`。工具不会自动挑选或合并贡献。

如果 `export` 后、推送前恰好合入了公开 PR，普通推送会拒绝。此时本地未发布候选与远端 PR 可能都以同一个已发布 commit 为父提交。默认 `adopt` 也会拒绝这种分叉，因为工具不能自动分辨候选从未发布，还是曾经发布后被远端重写。

确认该次推送失败、**这个确切本地候选从未发布**，并已审查、导入远端贡献后，可以显式恢复。`FULL_UNPUBLISHED_SHA` 必须替换为本地候选的完整 SHA：

```powershell
git -C output/github-public rev-parse HEAD
python scripts/release/public_snapshot.py adopt --unpublished-candidate FULL_UNPUBLISHED_SHA
```

此声明不跳过历史核对：工具仍核对已发布基线的本地审计、候选唯一父提交和远端血缘，再把未发布候选保留到本地 `refs/quenching-unpublished/<候选SHA>`。该引用不会由 `main:main` 推送，也不是新的开发分支。此模式只替换从未发布的本地候选；不得用于接受已发布历史的重写或回退。默认仍要求私有源码树与已审查的远端树完整一致。

若私有 `main` 同时有尚未公开的开发改动，整个树通常与 PR 后的公开树不同。先人工审查并将该公开 commit 的改动导入私有 `main`，处理冲突、测试并提交，再使用显式审查声明。下例中的 `FULL_REMOTE_SHA` 必须替换为刚完成审查和导入的远端完整 SHA：

```powershell
git -C output/github-public ls-remote origin refs/heads/main
python scripts/release/public_snapshot.py adopt --reviewed-public-commit FULL_REMOTE_SHA --message "Integrate reviewed public changes"
git -C output/github-public diff HEAD^ HEAD
git -C output/github-public log -2 --oneline
git -C output/github-public push origin main:main
python scripts/release/public_snapshot.py status
```

`--reviewed-public-commit` 表示操作者确认**这个确切公开提交已经审查并导入私有源码**，不是跳过导入的开关。工具要求 SHA 与当前远端 `main` 完全一致，并继续核对公开血缘、私有历史、源码提交和本地审计；远端发生变化、无关历史或未知本地提交仍会拒绝。若两份树不同，工具以已审查公开 commit 为父提交，用当前私有 `HEAD` 的完整文件树建立新的公开候选，输出 `action: reconciled`。工具无法自动证明人工导入正确，推送前必须审查候选相对父提交的完整 diff，确认贡献被保留且额外开发改动可以公开。若同时有上述推送竞争，需要在同一条 `adopt` 命令中再加 `--unpublished-candidate FULL_UNPUBLISHED_SHA`，分别声明候选从未发布与远端贡献已经导入。此命令不推送；有新的推送竞争时，再次审查新远端并重复流程。

## Tag 和验证

发布 Tag 在独立公开仓库创建，并指向已审查的公开 commit。私有/Gitee 的同名 Tag 可以对应私有源码 commit，依据审计中的树哈希确认同一份源码；不要从私有仓库向 GitHub 推送历史、Tag、`--all` 或 `--mirror`。版本和 Release Notes 遵循 [发布约定](../../docs/releases.md)。

离线回归全部使用临时的本地 Git 仓库和裸远端，不访问 GitHub/Gitee：

```powershell
python -m unittest discover -s scripts/release -p "test_*.py" -v
```
