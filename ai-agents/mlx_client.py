"""mlx_client.py — mlx-serve (OpenAI 互換) を呼ぶバッチツール共通の HTTP ヘルパー。

mlx-serve (~/AI/MLX) の注意点:
- mlx-vlm は ``model`` 必須。別の名前を送ると常駐モデルが入れ替わるため、
  プロファイルの model_id を必ず送る
- seed を送らないと同じ入力に毎回同じ文を返すため、毎回ランダムな seed を送る
- thinking は明示的に無効化する (エンジンごとにキーが違うので両方送る)
"""

import json
import random
import urllib.request

DEFAULT_LLM_URL = "http://127.0.0.1:11441"
DEFAULT_LLM_MODEL_ID = "gemma4-31b-it-heretic-4bit-mtp"
DEFAULT_EMBED_URL = "http://127.0.0.1:11455"
DEFAULT_EMBED_MODEL_ID = "bge-m3-q8"


def http_post(url: str, payload: dict, timeout: int) -> dict:
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        url, data=data, headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=timeout) as res:
        return json.loads(res.read())


def chat(
    url: str,
    model_id: str,
    prompt: str,
    *,
    max_tokens: int,
    temperature: float,
    timeout: int,
    top_p: float | None = None,
    stop: list[str] | None = None,
) -> str:
    """プロンプトを user メッセージ 1 つとして送り、応答本文を返す。"""
    payload: dict = {
        "model": model_id,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "temperature": temperature,
        "seed": random.randrange(1 << 31),
        "enable_thinking": False,
        "chat_template_kwargs": {"enable_thinking": False},
    }
    if top_p is not None:
        payload["top_p"] = top_p
    if stop:
        payload["stop"] = stop
    result = http_post(f"{url}/v1/chat/completions", payload, timeout)
    return (result["choices"][0]["message"].get("content") or "").strip()


def embed(url: str, model_id: str, text: str, timeout: int = 30) -> list[float]:
    """テキスト 1 件の埋め込みベクトルを返す。"""
    result = http_post(
        f"{url}/v1/embeddings", {"model": model_id, "input": text}, timeout
    )
    return result["data"][0]["embedding"]


def ping(url: str) -> bool:
    try:
        urllib.request.urlopen(f"{url}/health", timeout=3)
        return True
    except OSError:
        return False
