#!/usr/bin/env python3
"""grok-http-registrar bridge: 驱动 reg-factory register_grok_http.py（纯 HTTP Grok 注册）。

依赖环境变量：
  REG_FACTORY_ROOT — reg-factory checkout 路径

平台能力注入：
  stdin JSON 的 env 字段（MAIL_ROUTER_*、YYDS_API_KEY、RESIN_*、YESCAPTCHA_* 等）
  会在 import reg-factory 之前写入 os.environ，从而覆盖 reg-factory .env 的
  对应配置（config._load_dotenv 不会覆盖已设置的环境变量）。
"""

import json
import os
import shutil
import sys
from pathlib import Path


def emit(typ: str, **kw) -> None:
    print(json.dumps({"type": typ, **kw}, ensure_ascii=False), flush=True)


def main() -> None:
    raw = sys.stdin.readline().strip()
    if not raw:
        emit("error", attempt=0, msg="stdin 空，未收到 job config")
        sys.exit(1)

    cfg = json.loads(raw)
    target = int(cfg.get("target", 1))
    sub2api = bool((cfg.get("config") or {}).get("sub2api", False))
    output_dir = Path(cfg.get("outputDir", "."))
    output_dir.mkdir(parents=True, exist_ok=True)

    # 1. 注入平台能力 env（覆盖语义：reg-factory 的 config._load_dotenv 只在
    #    key 尚未设置时填充，因此这里先写 os.environ 即可生效）。
    for k, v in (cfg.get("env") or {}).items():
        os.environ[k] = v

    # 2. 再 import reg-factory（config.py 此时会读到注入的能力配置）。
    rf_root = os.environ.get(
        "REG_FACTORY_ROOT", "/Users/talexdreamsoul/Workspace/Deploy/reg-factory"
    )
    sys.path.insert(0, rf_root)
    try:
        from register_grok_http import register_one
    except ImportError as e:
        emit(
            "error", attempt=0,
            msg=f"无法导入 register_grok_http: {e}. REG_FACTORY_ROOT={rf_root}", email="",
        )
        sys.exit(2)

    tokens_dir = Path(rf_root) / "tokens" / "grok"
    before = {f.name for f in tokens_dir.glob("*.sso.json")} if tokens_dir.is_dir() else set()

    ok = fail = 0
    for i in range(1, target + 1):
        emit("log", msg=f"#{i}/{target} Grok HTTP 注册...")
        try:
            sso = register_one(i, target, sub2api=sub2api)
            if sso:
                ok += 1
            else:
                emit("error", attempt=i, msg="注册返回空")
                fail += 1
        except Exception as e:
            emit("error", attempt=i, msg=str(e)[:200])
            fail += 1
        emit("progress", done=i, total=target)

    # 摄入新产出的 sso token 文件
    after = {f.name for f in tokens_dir.glob("*.sso.json")} if tokens_dir.is_dir() else set()
    for name in sorted(after - before):
        src = tokens_dir / name
        dst = output_dir / name
        shutil.copy2(src, dst)
        email = name[: -len(".sso.json")]
        emit("artifact", kind="session.sso", file=name, email=email)

    emit("done", ok=ok, fail=fail, total=target)
    sys.exit(0 if fail == 0 else 1)


if __name__ == "__main__":
    main()
