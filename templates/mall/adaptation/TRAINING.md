# mall 源码与运行目录

本目录是当前实训班次的可编辑源码。“文件”跟随终端当前路径，也可通过“项目目录 / 部署目录 / 配置目录”查看。

| 服务 | 源码 | 运行文件 | 当前配置 |
| --- | --- | --- | --- |
| frontend | `/workspace/mall-app-web`，页面在 `src/pages` | `/usr/share/nginx/html` | `/etc/nginx/conf.d/default.conf` |
| mall-portal | `/workspace/mall`，业务代码在 `mall-portal/src/main/java` | `/app/mall-portal.jar` | `/app/config/application-sandbox.yml`、`logback-sandbox.xml` |

修改、保存源码后，在当前服务器终端执行：

```sh
training-rebuild
```

前端使用现有依赖离线构建，成功后更新 HTML 并重载 Nginx。Java 构建期间暂停应用以腾出内存，成功后替换 JAR 并启动应用；编译失败继续使用上次发布结果，源码修改仍保留。Java 重启后还需等待健康检查，成功编译不代表业务已经恢复。

源码、可编辑配置和发布结果保存在本班次 `/workspace` 卷中，容器重启后保留，结束实训后随沙箱回收。下次从模板基线初始化。不要删除 `.training-initialized` 标记，删除它会触发初始化。

`node_modules` 和 Maven 仓库使用镜像预置缓存。运行时不访问公网下载依赖；新增未缓存依赖需由模板维护者更新镜像。

配置修改后需要对应应用重新加载。Java 可运行 `training-rebuild`；Nginx 先运行 `nginx -t`，通过后运行 `nginx -s reload`。请结合日志、监控和真实交易验证效果。
