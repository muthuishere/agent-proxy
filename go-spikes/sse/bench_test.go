package sse

import "testing"

func BenchmarkPerEventBuffer(b *testing.B) {
	secret := "sk-ant-api03-abcdefghijklmnop"
	chunks := FixtureChunkSplit(secret)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectAcrossChunks(chunks, secret, PerEventBuffer, 0)
	}
}

func BenchmarkFullBuffer(b *testing.B) {
	secret := "sk-ant-api03-abcdefghijklmnop"
	chunks := FixtureChunkSplit(secret)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectAcrossChunks(chunks, secret, FullBuffer, 0)
	}
}

func BenchmarkSlidingWindow(b *testing.B) {
	secret := "sk-ant-api03-abcdefghijklmnop"
	chunks := FixtureChunkSplit(secret)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectAcrossChunks(chunks, secret, SlidingWindow, len(secret))
	}
}
