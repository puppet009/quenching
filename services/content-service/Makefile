.PHONY: run build test lint migrate-up migrate-down

# content-service 常用命令。其他 Go 服务照抄，改服务名即可。
SERVICE := content-service

run:
	go run ./cmd/server

build:
	go build -o bin/$(SERVICE) ./cmd/server

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...

# 迁移工具：golang-migrate。DB_DSN 可用环境变量覆盖。
# 迁移表名必须与根 Makefile 的 migrate 目标推出的名字一致（schema_migrations_<service>），
# 否则本目录的 migrations 会有两份互不相知的版本号。需 golang-migrate >= 4.15。
DB_DSN ?= postgres://quenching:quenching@localhost:5432/quenching?sslmode=disable
MIGRATIONS_TABLE := schema_migrations_$(shell echo $(SERVICE) | tr - _)

migrate-up:
	@echo "首次安装请先在仓库根执行 make migrate，以完成历史源域迁移。"
	@case "$(DB_DSN)" in *\?*) sep='&';; *) sep='?';; esac; \
		migrate -path migrations -database "$(DB_DSN)$$sep""x-migrations-table=$(MIGRATIONS_TABLE)" up

migrate-down:
	@echo "包含属主迁移的库禁止自动 down；按 deploy/server/近期升级说明.md 从备份回滚。"; exit 1
