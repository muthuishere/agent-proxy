package codec

import "testing"

func TestEncodeDecodeBodyRoundTripGzip(t *testing.T) {
	original := `{"prompt":"hello"}`

	encoded := EncodeBody(original, "gzip")
	decoded := DecodeBody(encoded, "gzip")

	if decoded != original {
		t.Fatalf("unexpected decoded body: %q", decoded)
	}
}

func TestDecodeBodyFallsBackOnBrokenGzip(t *testing.T) {
	decoded := DecodeBody([]byte("not-gzip"), "gzip")
	if decoded != "not-gzip" {
		t.Fatalf("expected raw body fallback, got %q", decoded)
	}
}
