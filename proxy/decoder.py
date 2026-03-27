import base64
import gzip
import re
from urllib.parse import quote, unquote

_BASE64_RE = re.compile(r'(?<![A-Za-z0-9+/])([A-Za-z0-9+/]{40,}={0,2})(?![A-Za-z0-9+/=])')
_HEX_RE = re.compile(r'(?<![0-9a-fA-F])([0-9a-fA-F]{40,})(?![0-9a-fA-F])')
_URL_ENCODED_BLOB_RE = re.compile(
    r'((?:[A-Za-z0-9._~\-]|%[0-9a-fA-F]{2}){12,}%[0-9a-fA-F]{2}(?:[A-Za-z0-9._~\-]|%[0-9a-fA-F]{2})*)'
)

MAX_DECODE_DEPTH = 4


def decode_body(content: bytes, content_encoding: str = "", content_type: str = "") -> str:
    if "gzip" in content_encoding:
        try:
            content = gzip.decompress(content)
        except Exception:
            pass
    try:
        return content.decode("utf-8", errors="replace")
    except Exception:
        return ""


def encode_body(text: str, content_encoding: str = "") -> bytes:
    encoded = text.encode("utf-8")
    if "gzip" in content_encoding:
        return gzip.compress(encoded)
    return encoded


def decode_encoded_blobs(text: str, scanner) -> tuple[str, int]:
    """
    Find base64 / hex / url-encoded blobs in text, decode them,
    scan for secrets, re-encode with masks applied.
    Returns (modified_text, total_masks_applied).
    Multi-pass up to MAX_DECODE_DEPTH to handle double-encoding.
    """
    total_masked = 0
    for _ in range(MAX_DECODE_DEPTH):
        text, masked = _decode_pass(text, scanner)
        total_masked += masked
        if masked == 0:
            break
    return text, total_masked


def _decode_pass(text: str, scanner) -> tuple[str, int]:
    masked = 0

    # --- URL-encoded blobs ---
    for m in list(_URL_ENCODED_BLOB_RE.finditer(text)):
        blob = m.group(1)
        decoded = unquote(blob)
        if decoded == blob:
            continue
        decoded, inner = _decode_pass(decoded, scanner)
        hits = scanner.scan_text(decoded)
        if not hits and not inner:
            continue
        masked_decoded = decoded
        for hit in hits:
            if hit.value in masked_decoded:
                masked_decoded = masked_decoded.replace(hit.value, f"[MASKED:{hit.name}]")
                masked += 1
        masked += inner
        re_encoded = quote(masked_decoded, safe="")
        text = text[:m.start(1)] + re_encoded + text[m.end(1):]

    # --- Base64 blobs ---
    for m in list(_BASE64_RE.finditer(text)):
        blob = m.group(1)
        try:
            decoded_bytes = base64.b64decode(blob + "==")
            decoded_str = decoded_bytes.decode("utf-8", errors="replace")
        except Exception:
            continue
        # recurse into the decoded content before scanning
        decoded_str, inner = _decode_pass(decoded_str, scanner)
        hits = scanner.scan_text(decoded_str)
        if not hits and not inner:
            continue
        masked_decoded = decoded_str
        for hit in hits:
            if hit.value in masked_decoded:
                masked_decoded = masked_decoded.replace(hit.value, f"[MASKED:{hit.name}]")
                masked += 1
        masked += inner
        re_encoded = base64.b64encode(masked_decoded.encode("utf-8")).decode("ascii")
        text = text[:m.start(1)] + re_encoded + text[m.end(1):]

    # --- Hex blobs ---
    for m in list(_HEX_RE.finditer(text)):
        blob = m.group(1)
        if len(blob) % 2 != 0:
            continue
        try:
            decoded_bytes = bytes.fromhex(blob)
            decoded_str = decoded_bytes.decode("utf-8", errors="replace")
        except Exception:
            continue
        decoded_str, inner = _decode_pass(decoded_str, scanner)
        hits = scanner.scan_text(decoded_str)
        if not hits and not inner:
            continue
        masked_decoded = decoded_str
        for hit in hits:
            if hit.value in masked_decoded:
                masked_decoded = masked_decoded.replace(hit.value, f"[MASKED:{hit.name}]")
                masked += 1
        masked += inner
        re_encoded = masked_decoded.encode("utf-8").hex()
        text = text[:m.start(1)] + re_encoded + text[m.end(1):]

    return text, masked
