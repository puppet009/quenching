"""ai-service 配置。从环境变量读取，MVP 默认 MOCK 模式（不调真实模型、不产生费用）。"""

from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env", env_file_encoding="utf-8", extra="ignore"
    )

    app_name: str = "ai-service"
    app_env: str = "development"
    internal_token: str = ""
    port: int = 8000
    log_level: str = "info"
    # 日志根目录；为空则只输出 stdout，集群由 Helm 挂载宿主机 logs 或 PVC。
    log_dir: str = ""
    # 文件保留天数（按天轮转）。
    log_retention_days: int = 30

    # gRPC（供 Go 服务调用；与 FastAPI 的 HTTP 端口分开）
    grpc_host: str = "0.0.0.0"
    grpc_port: int = 9000
    enable_reflection: bool = True

    # LLM
    # T4-16：默认模型切 qwen3.8-flash，走 OpenAI 兼容 /v1/chat/completions；
    # base_url/model/api_key 全部由云端环境变量（LLM_BASE_URL/LLM_MODEL/LLM_API_KEY）
    # 按实际网关地址与模型 ID 注入，此处仅占位默认值。
    llm_mock: bool = True  # True=本地假回复，不花钱；云端显式设 False
    llm_provider: str = "qwen"
    llm_api_key: str = ""
    llm_base_url: str = "https://dashscope.aliyuncs.com/compatible-mode"
    llm_model: str = "qwen3.8-flash"
    # 单次调用最长 300s（长复盘六章一次返回）；超时不重试，避免墙钟翻倍。
    llm_timeout: float = 300.0
    # 仅连接类错误/429/5xx 等快速失败时重试，按“总尝试次数”计（含首次）。
    llm_max_retries: int = 3
    llm_director_timeout: float = 60.0
    llm_assistant_timeout: float = 30.0
    llm_memory_timeout: float = 60.0
    llm_assessment_timeout: float = 90.0
    llm_review_timeout: float = 300.0
    # 成本估算单价（元/百万 token），仅用于用量统计；0 表示暂不计费，
    # 云端可按所用网关的实际 flash 单价经环境变量覆盖。
    llm_price_input: float = 0.0
    llm_price_output: float = 0.0


@lru_cache
def get_settings() -> Settings:
    return Settings()
