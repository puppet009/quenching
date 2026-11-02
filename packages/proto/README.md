# proto

服务间契约的**唯一来源**。proto 文件按 package 分目录存放（buf `PACKAGE_DIRECTORY_MATCH`）：

```
quenching/ai/v1/ai_service.proto   # package quenching.ai.v1
```

- `ai_service.proto`：Go 服务 ↔ Python ai-service 的 gRPC 契约（Chat / GenerateReview / EvaluateCollab）。EvaluateCollab 为协作行为三维评估的确定性规则接口（不调 LLM），仅服务间调用，不对外暴露 HTTP。
- HTTP 对外接口契约用 OpenAPI 3.0 编写（见各服务 `api/v1/openapi.yaml`）。

## 目录与生成产物

| 语言 | 生成产物 | 位置 |
| --- | --- | --- |
| Go | `aiv1/ai_service.pb.go`、`aiv1/ai_service_grpc.pb.go` | 本 module 根（`go_package=.../aiv1`），**提交仓库** |
| Python | `quenching/ai/v1/ai_service_pb2.py(_grpc/.pyi)` | `services/ai-service/app/grpc_gen/`，**提交仓库** |

> 生成产物提交仓库：普通开发者无需安装 protoc/buf，clone 后即可编译运行。只有改契约时才需要下面的工具链。

## 工具链（仅改契约时需要）

- Go：`buf` + `protoc-gen-go`、`protoc-gen-go-grpc`（`go install google.golang.org/protobuf/cmd/protoc-gen-go@latest` 等）。
- Python：`pip install grpcio-tools`。

## 重新生成

在本目录（`packages/proto`）执行：

```bash
# 校验
buf lint

# Go：生成到 ./aiv1
buf generate

# Python：生成到 ai-service（先确保输出目录存在）
mkdir -p ../../services/ai-service/app/grpc_gen
python -m grpc_tools.protoc -I. \
  --python_out=../../services/ai-service/app/grpc_gen \
  --grpc_python_out=../../services/ai-service/app/grpc_gen \
  --pyi_out=../../services/ai-service/app/grpc_gen \
  quenching/ai/v1/ai_service.proto
```

Python 生成代码使用与 proto package 一致的绝对导入（`from quenching.ai.v1 import ...`），
由 `services/ai-service/app/grpc_gen/__init__.py` 在导入时把生成目录加入 `sys.path` 解析，**不要手改生成文件**。

## 联调

```bash
# 1) 启动 Python gRPC server（默认 0.0.0.0:9000，可用 GRPC_PORT 覆盖）
cd services/ai-service
python -m app.grpc_server.server

# 2) Go 端集成测试（需 -tags integration，默认不跑）
cd services/training-service
go test -tags integration ./internal/ai/ -run TestAI -v
```

> 前端 TypeScript 类型从 OpenAPI 自动生成（openapi-typescript），**禁止手写 shared-types**。

## 导演契约与无本地环境生成

BrainDecide 是单班次导演推理接口，使用 x-internal-token 鉴权；不与学员 Chat 共享会话。响应记录实际模型、请求 ID 和用量。

已有 buf 工具可执行 `buf generate --template buf.python.gen.yaml`，用固定远端 compiler 自动生成 Python 文件，无需本地安装 grpcio-tools。当前生成器：protobuf 6.31.1、gRPC 1.73.1；运行时 requirements.txt 的版本应不低于生成器。Go 用 `buf generate`。生成代码禁止手工修改。

## JSON 兼容契约

operationevidence、reviewcontract 和 contentcontract 存放已有 HTTP JSON 接口的传输结构与协议编解码/校验，不包含数据库、运行时 handler 或领域写入逻辑。与生成的 gRPC 消息分开；本次不改生成文件或已有 HTTP 外观。testutil 仅作端口验证夹具，不参与生产装配。
