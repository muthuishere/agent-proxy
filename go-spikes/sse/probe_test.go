package sse

import "testing"

func TestChunkSplitDetection(t *testing.T) {
	secret := "sk-ant-api03-abcdefghijklmnop"
	chunks := FixtureChunkSplit(secret)

	perEvent := DetectAcrossChunks(chunks, secret, PerEventBuffer, 0)
	if !perEvent.Found {
		t.Fatalf("expected per-event strategy to detect secret across chunk split")
	}

	full := DetectAcrossChunks(chunks, secret, FullBuffer, 0)
	if !full.Found {
		t.Fatalf("expected full-buffer strategy to detect secret")
	}

	sliding := DetectAcrossChunks(chunks, secret, SlidingWindow, len(secret))
	if !sliding.Found {
		t.Fatalf("expected sliding-window strategy to detect secret")
	}
}

func TestDataBoundarySplitShowsPerEventRisk(t *testing.T) {
	secret := "sk-ant-api03-abcdefghijklmnop"
	chunks := FixtureDataBoundarySplit(secret)

	perEvent := DetectAcrossChunks(chunks, secret, PerEventBuffer, 0)
	if perEvent.Found {
		t.Fatalf("expected per-event strategy to miss data-boundary split secret")
	}

	full := DetectAcrossChunks(chunks, secret, FullBuffer, 0)
	if full.Found {
		t.Fatalf("expected full-buffer raw scan to miss data-boundary split secret")
	}

	sliding := DetectAcrossChunks(chunks, secret, SlidingWindow, len(secret))
	if sliding.Found {
		t.Fatalf("expected sliding-window raw scan to miss data-boundary split secret")
	}
}

func TestSemanticNormalizationCatchesDataBoundarySplit(t *testing.T) {
	secret := "sk-ant-api03-abcdefghijklmnop"
	chunks := FixtureDataBoundarySplit(secret)

	result := DetectWithSemanticNormalization(chunks, secret)
	if !result.Found {
		t.Fatalf("expected semantic normalization to detect data-boundary split secret")
	}
	if !result.StructurePreserved {
		t.Fatalf("expected structure to be preserved after semantic normalization")
	}
	if result.EventCount != 1 {
		t.Fatalf("expected 1 event, got %d", result.EventCount)
	}
}

func TestSemanticNormalizationIgnoresNonDataLines(t *testing.T) {
	secret := "postgres://user:pass@host:5432/db"
	chunks := []string{
		"id: 1\nevent: ping\n\n",
		"event: message\ndata: " + secret + "\n\n",
	}

	result := DetectWithSemanticNormalization(chunks, secret)
	if !result.Found {
		t.Fatalf("expected semantic normalization to detect secret in data: line")
	}
	if result.EventCount != 2 {
		t.Fatalf("expected 2 events, got %d", result.EventCount)
	}
}

func TestStructurePreservedAcrossStrategies(t *testing.T) {
	secret := "postgres://user:pass@host:5432/db"
	chunks := []string{
		"id: 1\n",
		"event: message\ndata: " + secret + "\n\n",
	}

	for _, strategy := range []Strategy{PerEventBuffer, FullBuffer, SlidingWindow} {
		result := DetectAcrossChunks(chunks, secret, strategy, len(secret))
		if !result.StructurePreserved {
			t.Fatalf("expected structure preserved for %s", strategy)
		}
	}
}
