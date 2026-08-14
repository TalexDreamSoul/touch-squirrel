#!/usr/bin/env python3
"""github-registrar bridge runner for touch-squirrel host.

Called by the Go host via os/exec. Reads one JSON line from stdin containing
job config, emits NDJSON events on stdout, writes artifacts to outputDir.

Expected environment:
  REG_FACTORY_ROOT — path to reg-factory checkout (contains register_github.py + common/)
"""

import asyncio
import json
import os
import sys
from pathlib import Path


# ---------------------------------------------------------------------------
# Resolve reg-factory import root
# ---------------------------------------------------------------------------
RF_ROOT = os.environ.get(
    "REG_FACTORY_ROOT",
    str(Path(__file__).resolve().parents[3]),  # default: repo root
)
sys.path.insert(0, RF_ROOT)
sys.path.insert(0, os.path.join(RF_ROOT, "common"))

try:
    from register_github import register_one, load_pool_accounts
except ImportError as e:
    print(json.dumps({
        "type": "error",
        "attempt": 0,
        "msg": f"无法导入 register_github: {e}. REG_FACTORY_ROOT={RF_ROOT}",
        "email": "",
    }), flush=True)
    sys.exit(2)


def emit(typ: str, **kwargs) -> None:
    """Write one NDJSON event line to stdout."""
    print(json.dumps({"type": typ, **kwargs}, ensure_ascii=False), flush=True)


# ---------------------------------------------------------------------------
def main() -> None:
    raw = sys.stdin.readline().strip()
    if not raw:
        emit("error", attempt=0, msg="stdin 为空，未收到 job config")
        sys.exit(1)

    config = json.loads(raw)
    target = int(config.get("target", 5))
    plugin_cfg = config.get("config", {})
    auto = bool(plugin_cfg.get("auto", True))
    keep = bool(plugin_cfg.get("keepWindows", False))
    output_dir = Path(config.get("outputDir", "."))
    output_dir.mkdir(parents=True, exist_ok=True)

    # Merge bridge env into os.environ for register_github to pick up.
    for k, v in (config.get("env") or {}).items():
        if k not in os.environ:
            os.environ[k] = v

    # Load email pool.
    try:
        accounts = load_pool_accounts()
    except Exception as e:
        emit("error", attempt=0, msg=f"加载邮箱池失败: {e}")
        sys.exit(2)

    if not accounts:
        emit("error", attempt=0, msg="邮箱池为空（请确保 OUTLOOK_POOL_DIR 指向含 *.json 的目录）")
        sys.exit(2)

    batch = min(target, len(accounts))
    emit("log", msg=f"batch: {batch} of {len(accounts)} pool accounts")

    ok = 0
    fail = 0

    async def run() -> None:
        nonlocal ok, fail
        from playwright.async_api import async_playwright

        async with async_playwright() as p:
            # Ask register_one to skip variant retry loop (host does that).
            for attempt in range(1, min(target, 16) + 1):
                idx = attempt - 1
                email, password, cookies = accounts[idx % len(accounts)]

                emit("log", msg=f"#{attempt}/{target} email={email}")

                try:
                    result = await register_one(
                        email, password, cookies, p,
                        auto=auto, keep=keep,
                    )
                except Exception as e:
                    emit("error", attempt=attempt, msg=str(e)[:200], email=email)
                    fail += 1
                    emit("progress", done=attempt, total=batch, email=email)
                    continue

                if result is None:
                    emit("error", attempt=attempt, msg="注册返回 None", email=email)
                    fail += 1
                elif result == "SKIP_VARIANT":
                    emit("log", msg="  Arkose 难变体，跳过此窗口重试")
                    fail += 1
                elif result == "CAPTCHA_REACHED":
                    emit("log", msg="  停在验证步（explore 模式）")
                    fail += 1
                elif isinstance(result, str):
                    # register_one returns session cookie string on success.
                    # Construct a structured artifact.
                    username = f"github_{email.split('@')[0]}"
                    artifact = {
                        "email": email,
                        "username": username,
                        "session_cookie": result,
                        "platform": "github",
                    }
                    artifact_path = output_dir / f"{username}.json"
                    artifact_path.write_text(json.dumps(artifact, indent=2, ensure_ascii=False))
                    emit("artifact", kind="session.cookie",
                         file=str(artifact_path),
                         email=email,
                         username=username)
                    ok += 1
                else:
                    emit("error", attempt=attempt,
                         msg=f"未知返回类型: {type(result).__name__}", email=email)
                    fail += 1

                emit("progress", done=attempt, total=batch, email=email)

    asyncio.run(run())

    emit("done", ok=ok, fail=fail, total=batch)
    sys.exit(0 if fail == 0 else 1)


if __name__ == "__main__":
    main()
