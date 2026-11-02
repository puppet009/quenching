# admin 管理端镜像。构建上下文为仓库根（workspace 依赖 @quenching/shared）：
#   docker build -f apps/admin/Dockerfile -t quenching/admin:0.1.0-alpha.1 .
FROM node:22-alpine AS build
WORKDIR /repo
RUN corepack enable && corepack prepare pnpm@9.15.9 --activate

# 先复制 workspace 清单，利用层缓存安装依赖
COPY pnpm-workspace.yaml pnpm-lock.yaml package.json ./
COPY packages/shared/package.json packages/shared/
COPY apps/admin/package.json apps/admin/
RUN pnpm install --filter @quenching/admin... --frozen-lockfile

# 复制源码并构建
COPY packages/shared packages/shared
COPY apps/admin apps/admin
RUN pnpm --filter @quenching/admin build

# ------------------------------ 运行阶段 -----------------------------------
FROM nginx:1.27-alpine
RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai
COPY apps/admin/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/apps/admin/dist /usr/share/nginx/html
EXPOSE 80
