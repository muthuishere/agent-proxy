package goproxy

import (
	"bytes"
	"testing"
)

func BenchmarkCopyWebSocketFrameOnce(b *testing.B) {
	payload := []byte(`{"database_url":"postgres://user:secret@host:5432/db","aws_secret_access_key":"SECRETKEY1234567890","message":"hello"}`)

	buildFrame := func() []byte {
		var src bytes.Buffer
		if err := writeWebSocketFrame(&src, WebSocketFrame{FIN: true, Opcode: 0x1, Payload: payload}, true); err != nil {
			b.Fatalf("build frame: %v", err)
		}
		return src.Bytes()
	}

	b.Run("raw_copy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			frameBytes := buildFrame()
			var dst bytes.Buffer
			if err := copyWebSocketFrameOnce(&dst, bytes.NewReader(frameBytes), nil); err != nil {
				b.Fatalf("copy frame: %v", err)
			}
		}
	})

	b.Run("mutate_text_frame", func(b *testing.B) {
		handler := func(ctx *ProxyCtx, direction WebSocketDirection, frame WebSocketFrame) (WebSocketFrame, error) {
			frame.Payload = bytes.ReplaceAll(frame.Payload, []byte("postgres://user:secret@host:5432/db"), []byte("post************************db[GENERIC_CONNECTION_STRING:bench]"))
			return frame, nil
		}
		for i := 0; i < b.N; i++ {
			frameBytes := buildFrame()
			var dst bytes.Buffer
			if err := copyWebSocketFrameOnce(&dst, bytes.NewReader(frameBytes), handler); err != nil {
				b.Fatalf("copy frame: %v", err)
			}
		}
	})
}

func BenchmarkCopyWebSocketFrameOnceParallel(b *testing.B) {
	payload := []byte(`{"database_url":"postgres://user:secret@host:5432/db","aws_secret_access_key":"SECRETKEY1234567890","message":"hello"}`)

	buildFrame := func() []byte {
		var src bytes.Buffer
		if err := writeWebSocketFrame(&src, WebSocketFrame{FIN: true, Opcode: 0x1, Payload: payload}, true); err != nil {
			b.Fatalf("build frame: %v", err)
		}
		return src.Bytes()
	}

	handler := func(ctx *ProxyCtx, direction WebSocketDirection, frame WebSocketFrame) (WebSocketFrame, error) {
		frame.Payload = bytes.ReplaceAll(frame.Payload, []byte("postgres://user:secret@host:5432/db"), []byte("post************************db[GENERIC_CONNECTION_STRING:bench]"))
		return frame, nil
	}

	b.Run("raw_copy_parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				frameBytes := buildFrame()
				var dst bytes.Buffer
				if err := copyWebSocketFrameOnce(&dst, bytes.NewReader(frameBytes), nil); err != nil {
					b.Fatalf("copy frame: %v", err)
				}
			}
		})
	})

	b.Run("mutate_text_frame_parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				frameBytes := buildFrame()
				var dst bytes.Buffer
				if err := copyWebSocketFrameOnce(&dst, bytes.NewReader(frameBytes), handler); err != nil {
					b.Fatalf("copy frame: %v", err)
				}
			}
		})
	})
}
