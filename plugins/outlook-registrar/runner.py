#!/usr/bin/env python3
"""outlook-registrar bridge: 驱动 reg-factory outlook_reg_loop.py（Outlook 邮箱常驻注册）。"""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

RF_ROOT = os.environ.get(
    "REG_FACTORY_ROOT", "/Users/talexdreamsoul/Workspace/Deploy/reg-factory"
)


def emit(typ: str, **kw) -> None:
    print(json.dumps({"type": typ, **kw}, ensure_ascii=False), flush=True)


def main() -> None:
    raw = sys.stdin.readline().strip()
    if not raw:
        emit("error", attempt=0, msg="stdin 空，未收到 job config")
        sys.exit(1)

    cfg = json.loads(raw)
    target = int(cfg.get("target", 1))
    no_rotate = bool((cfg.get("config") or {}).get("noRotate", False))
    output_dir = Path(cfg.get("outputDir", "."))
    output_dir.mkdir(parents=True, exist_ok=True)

    for k, v in (cfg.get("env") or {}).items():
        if k not in os.environ:
            os.environ[k] = v

    root = Path(RF_ROOT)
    py = os.environ.get("GROK_PYTHON") or str(root / ".venv" / "bin" / "python")
    cmd = [py, "-u", str(root / "outlook_reg_loop.py"), "--count", str(target)]
    if no_rotate:
        cmd.append("--no-rotate")
    emit("log", msg=f"执行: {' '.join(cmd)}")

    proc = subprocess.Popen(
        cmd, cwd=str(root),
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        text=True, bufsize=1,
    )
    assert proc.stdout is not None
    for line in proc.stdout:
        line = line.rstrip()
        if line:
            emit("log", msg=line)
    rc = proc.wait()
    if rc != 0:
        emit("error", attempt=0, msg=f"outlook_reg_loop 退出码 {rc}")
        emit("done", ok=0, fail=target, total=target)
        sys.exit(1)

    pool_dir = root / "_outlook_pool"
    files = sorted(pool_dir.glob("*.json")) if pool_dir.is_dir() else []
    ok = 0
    for f in files:
        dst = output_dir / f.name
        shutil.copy2(f, dst)
        try:
            data = json.loads(f.read_text())
            email = data.get("email", f.stem)
        except Exception:
            email = f.stem
        emit("artifact", kind="account.outlook", file=f.name, email=email)
        ok += 1

    emit("done", ok=ok, fail=max(0, target - ok), total=target)
    sys.exit(0 if ok > 0 else 1)


if __name__ == "__main__":
    main()
