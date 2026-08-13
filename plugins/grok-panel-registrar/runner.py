#!/usr/bin/env python3
"""grok-panel-registrar bridge: 驱动 grok-register-panel 批量注册并摄入 CPA 产物。

由 Go host 通过 os/exec 调用。stdin 收一行 JSON（job config），stdout 输出
NDJSON 事件，产物复制到 outputDir 后发 artifact 事件。

依赖环境变量：
  GROK_PANEL_ROOT — grok-register-panel checkout 路径（含 run_batch_headless.py + config.json）
  GROK_PYTHON     — 可选，Python 解释器（默认 GROK_PANEL_ROOT/.venv/bin/python）
"""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

DEFAULT_ROOT = "/Users/talexdreamsoul/grok-register-panel"


def emit(typ: str, **kw) -> None:
    print(json.dumps({"type": typ, **kw}, ensure_ascii=False), flush=True)


def main() -> None:
    raw = sys.stdin.readline().strip()
    if not raw:
        emit("error", attempt=0, msg="stdin 空，未收到 job config")
        sys.exit(1)

    cfg = json.loads(raw)
    target = int(cfg.get("target", 1))
    workers = int((cfg.get("config") or {}).get("workers", 1))
    output_dir = Path(cfg.get("outputDir", "."))
    output_dir.mkdir(parents=True, exist_ok=True)

    root = Path(os.environ.get("GROK_PANEL_ROOT", DEFAULT_ROOT))
    if not (root / "run_batch_headless.py").is_file():
        emit("error", attempt=0, msg=f"GROK_PANEL_ROOT 无效: {root}")
        sys.exit(2)

    py = os.environ.get("GROK_PYTHON") or str(root / ".venv" / "bin" / "python")
    script = str(root / "run_batch_headless.py")
    cmd = [py, "-u", script, str(target), str(workers)]
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
        emit("error", attempt=0, msg=f"run_batch_headless 退出码 {rc}")
        emit("done", ok=0, fail=target, total=target)
        sys.exit(1)

    cpa_dir = root / "cpa_auth"
    files = sorted(cpa_dir.glob("xai-*.json")) if cpa_dir.is_dir() else []
    ok = 0
    for f in files:
        dst = output_dir / f.name
        shutil.copy2(f, dst)
        email = f.stem[len("xai-"):]
        emit("artifact", kind="account.xai", file=f.name, email=email)
        ok += 1

    emit("done", ok=ok, fail=max(0, target - ok), total=target)
    sys.exit(0 if ok > 0 else 1)


if __name__ == "__main__":
    main()
