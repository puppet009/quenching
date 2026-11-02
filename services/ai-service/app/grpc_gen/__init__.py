"""protobuf 生成代码引导包。

生成的模块使用与 proto package 一致的绝对导入（`from quenching.ai.v1 import ...`），
因此导入本包时把生成目录加入 sys.path，使其可作为顶层命名空间包被解析。

生成命令见 packages/proto/README.md；本目录内 quenching/ 下均为生成产物，请勿手改。
"""
import sys
from pathlib import Path

_GEN_DIR = Path(__file__).resolve().parent
if str(_GEN_DIR) not in sys.path:
    sys.path.insert(0, str(_GEN_DIR))
