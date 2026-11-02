# 淬火 Monorepo 根命令。新成员上手：make deps-up && make migrate && make dev
.PHONY: deps-up deps-down migrate dev-content dev-ai dev-web dev-admin lint test proto images image-go image-ai

# Go 服务列表（镜像名前缀 quenching/）。
# g7 起 ws-gateway 已并入 api-gateway（同进程承载 /ws 与 /api/v1/events）。
GO_SERVICES := api-gateway content-service user-service training-service review-service fault-engine sandbox-service
IMAGE_TAG ?= dev

# 1. 起本地依赖（PostgreSQL/Redis/RabbitMQ/Prometheus；平台侧不用 ES，日志走 Loki）
deps-up:
	docker compose -f deploy/docker/docker-compose.yml up -d

deps-down:
	docker compose -f deploy/docker/docker-compose.yml down

# 2. 执行所有服务的数据库迁移
#    每个服务用独立的迁移表：6 个 -path 目录共用同一个库 quenching，而 golang-migrate
#    每库只记一行 schema_migrations(version)，共用会让第一个服务把版本号顶上去、
#    其余服务的迁移被静默跳过。表名规则须与 services/content-service/Makefile 一致。
#    每域迁移表通过 PostgreSQL URL 的 x-migrations-table 参数传入。
DB_DSN ?= postgres://quenching:quenching@localhost:5432/quenching?sslmode=disable

migrate:
	@for s in fault-engine training-service user-service content-service sandbox-service review-service; do \
		if ! ls services/$$s/migrations/*.sql >/dev/null 2>&1; then continue; fi; \
		t=schema_migrations_$$(echo $$s | tr - _); \
		case "$(DB_DSN)" in *\?*) sep='&';; *) sep='?';; esac; \
		echo ">> migrate $$s (迁移表 $$t)"; \
		migrate -path services/$$s/migrations -database "$(DB_DSN)$$sep""x-migrations-table=$$t" up || exit 1; \
	done

# 3. 本地运行各服务（开发期分别开终端）
dev-content:
	cd services/content-service && go run ./cmd/server

dev-ai:
	cd services/ai-service && uvicorn app.main:app --reload --port 8000

dev-web:
	cd apps/web && pnpm dev

dev-admin:
	cd apps/admin && pnpm dev

# 质量门禁
lint:
	python scripts/check_structure.py
	python scripts/qa/domain-boundaries.py
	golangci-lint run ./...
	cd services/ai-service && ruff check .

test:
	go test ./... -race

proto:
	@echo "从 packages/proto 生成 Go/Python stub（脚本待补 buf/protoc 配置）"

# 4. 构建平台镜像。
# Go 服务 go.mod 用 replace 指向 packages/*，故 Go 镜像构建上下文必须是仓库根；
# ai-service 是独立 Python 工程，上下文是其自身目录。
image-go:
	@for s in $(GO_SERVICES); do \
		echo ">> build quenching/$$s:$(IMAGE_TAG)"; \
		docker build -f services/$$s/Dockerfile -t quenching/$$s:$(IMAGE_TAG) . || exit 1; \
	done

image-ai:
	docker build -f services/ai-service/Dockerfile -t quenching/ai-service:$(IMAGE_TAG) services/ai-service

images: image-go image-ai
	@echo "全部平台镜像构建完成（tag=$(IMAGE_TAG)）"
