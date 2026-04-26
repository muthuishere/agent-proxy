package sse

import "strings"

type Strategy string

const (
	PerEventBuffer Strategy = "per_event"
	FullBuffer     Strategy = "full_buffer"
	SlidingWindow  Strategy = "sliding_window"
)

type Result struct {
	Strategy           Strategy
	Found              bool
	EventCount         int
	StructurePreserved bool
}

func DetectAcrossChunks(chunks []string, secret string, strategy Strategy, window int) Result {
	switch strategy {
	case PerEventBuffer:
		return detectPerEvent(chunks, secret)
	case FullBuffer:
		return detectFullBuffer(chunks, secret)
	case SlidingWindow:
		return detectSlidingWindow(chunks, secret, window)
	default:
		return Result{Strategy: strategy}
	}
}

func detectPerEvent(chunks []string, secret string) Result {
	var builder strings.Builder
	found := false
	events := 0
	structure := true

	for _, chunk := range chunks {
		builder.WriteString(chunk)
		for {
			buffer := builder.String()
			idx := strings.Index(buffer, "\n\n")
			if idx == -1 {
				break
			}
			event := buffer[:idx]
			if strings.Contains(event, secret) {
				found = true
			}
			if !strings.Contains(event, "data:") {
				structure = false
			}
			events++
			builder.Reset()
			builder.WriteString(buffer[idx+2:])
		}
	}

	return Result{
		Strategy:           PerEventBuffer,
		Found:              found,
		EventCount:         events,
		StructurePreserved: structure,
	}
}

func detectFullBuffer(chunks []string, secret string) Result {
	payload := strings.Join(chunks, "")
	events := 0
	structure := true
	for _, part := range strings.Split(payload, "\n\n") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		events++
		if !strings.Contains(part, "data:") {
			structure = false
		}
	}
	return Result{
		Strategy:           FullBuffer,
		Found:              strings.Contains(payload, secret),
		EventCount:         events,
		StructurePreserved: structure,
	}
}

func detectSlidingWindow(chunks []string, secret string, window int) Result {
	if window <= 0 {
		window = len(secret)
	}

	events := 0
	structure := true
	trailing := ""
	found := false

	for _, chunk := range chunks {
		combined := trailing + chunk
		if strings.Contains(combined, secret) {
			found = true
		}

		events += strings.Count(chunk, "\n\n")
		for _, part := range strings.Split(chunk, "\n\n") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			if !strings.Contains(part, "data:") && !strings.HasPrefix(part, "id:") && !strings.HasPrefix(part, "event:") {
				structure = false
			}
		}

		if len(combined) > window {
			trailing = combined[len(combined)-window:]
		} else {
			trailing = combined
		}
	}

	return Result{
		Strategy:           SlidingWindow,
		Found:              found,
		EventCount:         events,
		StructurePreserved: structure,
	}
}

// NormalizeEventData joins the payloads from all data: lines within a single SSE
// event into one string per the SSE specification (RFC 8895 §9.2.6).
// A secret that spans two consecutive data: lines is therefore visible as a
// contiguous substring in the returned string.
func NormalizeEventData(event string) string {
	var parts []string
	for _, line := range strings.Split(event, "\n") {
		line = strings.TrimRight(line, "\r")
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			parts = append(parts, after)
		} else if after, ok := strings.CutPrefix(line, "data:"); ok {
			parts = append(parts, after)
		}
	}
	// Join without separator so a secret that straddles two consecutive
	// data: lines is still visible as a contiguous substring.
	return strings.Join(parts, "")
}

// DetectWithSemanticNormalization uses per-event buffering and applies
// NormalizeEventData before scanning, which catches secrets split across
// multiple data: lines within the same SSE event.
func DetectWithSemanticNormalization(chunks []string, secret string) Result {
	var builder strings.Builder
	found := false
	events := 0
	structure := true

	for _, chunk := range chunks {
		builder.WriteString(chunk)
		for {
			buffer := builder.String()
			idx := strings.Index(buffer, "\n\n")
			if idx == -1 {
				break
			}
			event := buffer[:idx]
			normalized := NormalizeEventData(event)
			if strings.Contains(normalized, secret) {
				found = true
			}
			if !strings.Contains(event, "data:") {
				structure = false
			}
			events++
			builder.Reset()
			builder.WriteString(buffer[idx+2:])
		}
	}

	return Result{
		Strategy:           PerEventBuffer,
		Found:              found,
		EventCount:         events,
		StructurePreserved: structure,
	}
}

func FixtureChunkSplit(secret string) []string {
	return []string{
		"event: message\ndata: prefix " + secret[:len(secret)/2],
		secret[len(secret)/2:] + " suffix\n\n",
	}
}

func FixtureDataBoundarySplit(secret string) []string {
	return []string{
		"event: message\ndata: prefix " + secret[:len(secret)/2] + "\n",
		"data: " + secret[len(secret)/2:] + " suffix\n\n",
	}
}
