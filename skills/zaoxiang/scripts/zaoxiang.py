"""
zaoxiang.py — DashScope image generation core library.
T2I: z-image-turbo | I2I: qwen-image-2.0
"""

import os
import json
import base64
import urllib.request
import urllib.error
from pathlib import Path

API_KEY = os.environ.get("DASHSCOPE_API_KEY", "")
ENDPOINT = "https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation"
DEFAULT_OUT = "/tmp/zaoxiang_output.png"


def _call(payload: dict, timeout: int = 120) -> dict:
    if not API_KEY:
        raise RuntimeError("DASHSCOPE_API_KEY 未设置")
    req = urllib.request.Request(
        ENDPOINT,
        data=json.dumps(payload).encode(),
        headers={
            "Authorization": f"Bearer {API_KEY}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        body = json.loads(e.read())
        raise RuntimeError(f"HTTP {e.code}: {body.get('message', body)}")


def _save(img_url: str, out: str) -> str:
    Path(out).parent.mkdir(parents=True, exist_ok=True)
    urllib.request.urlretrieve(img_url, out)
    return out


def t2i(
    prompt: str,
    size: str = "1024*1024",
    prompt_extend: bool = False,
    out: str = DEFAULT_OUT,
    timeout: int = 120,
) -> tuple[str, str]:
    """Text-to-image via z-image-turbo.

    Returns:
        (img_url, local_path)
    """
    payload = {
        "model": "z-image-turbo",
        "input": {
            "messages": [
                {"role": "user", "content": [{"text": prompt}]}
            ]
        },
        "parameters": {
            "size": size,
            "prompt_extend": prompt_extend,
        },
    }
    body = _call(payload, timeout)
    img_url = body["output"]["choices"][0]["message"]["content"][0]["image"]
    local = _save(img_url, out)
    return img_url, local


def i2i(
    image_input: str,
    prompt: str,
    size: str = "1024*1024",
    out: str = DEFAULT_OUT,
    timeout: int = 120,
) -> tuple[str, str]:
    """Image-to-image via qwen-image-2.0.

    Args:
        image_input: 本地文件路径 or 网络 URL
        prompt: 修改指令
        size: 输出分辨率
        out: 保存路径
        timeout: 超时秒数

    Returns:
        (img_url, local_path)
    """
    # Resolve image to URL or base64
    if image_input.startswith("http://") or image_input.startswith("https://"):
        image_field = {"image": image_input}
    else:
        p = Path(image_input)
        suffix = p.suffix.lower().lstrip(".")
        mime = {"jpg": "jpeg", "jpeg": "jpeg", "png": "png", "webp": "webp"}.get(suffix, "jpeg")
        with open(p, "rb") as f:
            b64 = base64.b64encode(f.read()).decode()
        image_field = {"image": f"data:image/{mime};base64,{b64}"}

    payload = {
        "model": "qwen-image-2.0",
        "input": {
            "messages": [
                {
                    "role": "user",
                    "content": [
                        image_field,
                        {"text": prompt},
                    ],
                }
            ]
        },
        "parameters": {"size": size},
    }
    body = _call(payload, timeout)
    img_url = body["output"]["choices"][0]["message"]["content"][0]["image"]
    local = _save(img_url, out)
    return img_url, local
