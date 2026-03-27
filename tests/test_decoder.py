from proxy.decoder import decode_body, encode_body


def test_decode_plain():
    body = b"hello world"
    assert decode_body(body) == "hello world"


def test_encode_decode_roundtrip():
    text = "some request body"
    encoded = encode_body(text)
    assert decode_body(encoded) == text


def test_gzip_roundtrip():
    text = "compressed body content"
    encoded = encode_body(text, content_encoding="gzip")
    decoded = decode_body(encoded, content_encoding="gzip")
    assert decoded == text
