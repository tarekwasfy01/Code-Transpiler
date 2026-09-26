from __future__ import annotations

import json
import os
import platform
import subprocess
from importlib.resources import files
from pathlib import Path
from typing import Any, Dict, Iterable, List


class CodeTranspilerError(RuntimeError):
    pass


def _engine_path() -> Path:
    override = os.environ.get("CODE_TRANSPILER_ENGINE")
    if override:
        p = Path(override)
        if not p.is_file():
            raise CodeTranspilerError(f"CODE_TRANSPILER_ENGINE does not exist: {p}")
        return p

    system = platform.system().lower()
    machine = platform.machine().lower()
    if machine in {"amd64", "x86_64"}:
        arch = "amd64"
    else:
        raise CodeTranspilerError(f"Unsupported CPU architecture for bundled engine: {machine}")

    if system == "windows":
        name = f"codetranspiler-engine-windows-{arch}.exe"
    elif system == "linux":
        name = f"codetranspiler-engine-linux-{arch}"
    else:
        raise CodeTranspilerError(
            f"No bundled engine for {platform.system()} {platform.machine()}. "
            "Set CODE_TRANSPILER_ENGINE to a compatible headless engine executable."
        )
    return Path(str(files("codetranspiler").joinpath("_engine", name)))


def _run(requests: Iterable[Dict[str, Any]], timeout: int = 120) -> List[Dict[str, Any]]:
    exe = _engine_path()
    try:
        proc = subprocess.run(
            [str(exe)],
            input=json.dumps(list(requests)).encode("utf-8"),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            check=False,
        )
    except subprocess.TimeoutExpired as exc:
        raise CodeTranspilerError(f"Code Transpiler engine timed out after {timeout}s") from exc
    except OSError as exc:
        raise CodeTranspilerError(f"Could not launch Code Transpiler engine: {exc}") from exc

    if proc.returncode != 0:
        stderr = proc.stderr.decode("utf-8", errors="replace").strip()
        raise CodeTranspilerError(stderr or f"Engine exited with code {proc.returncode}")
    try:
        result = json.loads(proc.stdout.decode("utf-8"))
    except Exception as exc:
        raise CodeTranspilerError("Engine returned invalid JSON") from exc
    if not isinstance(result, list):
        raise CodeTranspilerError("Engine returned an unexpected response")
    return result


def request(source: str, target: str = "", code: str = "", mode: str = "", timeout: int = 120, **extra: Any) -> Dict[str, Any]:
    payload: Dict[str, Any] = {
        "id": "python-api",
        "source": source,
        "target": target,
        "code": code,
    }
    if mode:
        payload["mode"] = mode
    payload.update(extra)
    result = _run([payload], timeout=timeout)[0]
    if result.get("error"):
        raise CodeTranspilerError(str(result["error"]))
    return result
