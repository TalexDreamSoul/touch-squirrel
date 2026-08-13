#!/usr/bin/env python3
"""chatgpt-registrar bridge: 驱动 reg-factory register_chatgpt.py（Playwright + BitBrowser）。"""

import asyncio
import json
import os
import sys
from pathlib import Path

RF_ROOT = os.environ.get(
    "REG_FACTORY_ROOT", "/Users/talexdreamsoul/Workspace/Deploy/reg-factory"
)
sys.path.insert(0, RF_ROOT)
sys.path.insert(0, os.path.join(RF_ROOT, "common"))

try:
    from register_chatgpt import register_one
except ImportError as e:
    print(json.dumps({
        "type": "error", "attempt": 0,
        "msg": f"无法导入 register_chatgpt: {e}. REG_FACTORY_ROOT={RF_ROOT}", "email": "",
    }), flush=True)
    sys.exit(2)


def emit(typ: str, **kw) -> None:
    print(json.dumps({"type": typ, **kw}, ensure_ascii=False), flush=True)


def main() -> None:
    raw = sys.stdin.readline().strip()
    if not raw:
        emit("error", attempt=0, msg="stdin 空，未收到 job config")
        sys.exit(1)

    cfg = json.loads(raw)
    target = int(cfg.get("target", 1))
    output_dir = Path(cfg.get("outputDir", "."))
    output_dir.mkdir(parents=True, exist_ok=True)

    for k, v in (cfg.get("env") or {}).items():
        if k not in os.environ:
            os.environ[k] = v

    ok = fail = 0

    async def run() -> None:
        nonlocal ok, fail
        from playwright.async_api import async_playwright

        async with async_playwright() as p:
            for i in range(1, target + 1):
                emit("log", msg=f"#{i}/{target} ChatGPT 注册...")
                try:
                    result = await register_one(i, target, p)
                    if result:
                        artifact = {"platform": "chatgpt", "session_token": result}
                        path = output_dir / f"chatgpt_{i}.json"
                        path.write_text(json.dumps(artifact, ensure_ascii=False))
                        emit("artifact", kind="session.token", file=str(path))
                        ok += 1
                    else:
                        emit("error", attempt=i, msg="注册返回空")
                        fail += 1
                except Exception as e:
                    emit("error", attempt=i, msg=str(e)[:200])
                    fail += 1
                emit("progress", done=i, total=target)

    asyncio.run(run())
    emit("done", ok=ok, fail=fail, total=target)
    sys.exit(0 if fail == 0 else 1)


if __name__ == "__main__":
    main()
