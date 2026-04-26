package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
)

// sseEvent is one parsed SSE message. It is either a "structured" text-delta
// event (where we know the JSON shape and can manipulate `delta.text`) or an
// opaque event (we keep the raw bytes and pass through unchanged).
type sseEvent struct {
	raw       []byte // original event bytes including trailing "\n\n"
	isText    bool   // true if this event carries a text_delta we can rewrite
	preDataLn []byte // bytes before the data: line on its own line (e.g. "event: content_block_delta\n")
	dataObj   map[string]any
	textPath  []string // JSON path to the text field (always {"delta", "text"} today)
	text      string   // current text content (mutated during surrogate rewrite)
}

// parseSSEEvent parses one event's bytes (without the trailing blank line).
// Returns nil if the event is not a JSON-bearing text-delta.
func parseSSEEvent(eventBytes []byte) *sseEvent {
	ev := &sseEvent{raw: append([]byte(nil), eventBytes...)}
	// Find the data: line.
	lines := bytes.Split(eventBytes, []byte("\n"))
	var preData [][]byte
	var dataLine []byte
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("data:")) {
			dataLine = bytes.TrimPrefix(line, []byte("data:"))
			dataLine = bytes.TrimLeft(dataLine, " ")
			preData = lines[:i]
			break
		}
	}
	if dataLine == nil {
		return ev
	}
	// Parse the JSON.
	var obj map[string]any
	if err := json.Unmarshal(dataLine, &obj); err != nil {
		return ev
	}
	// Look for delta.text where delta.type == "text_delta".
	delta, ok := obj["delta"].(map[string]any)
	if !ok {
		return ev
	}
	if t, ok := delta["type"].(string); !ok || t != "text_delta" {
		return ev
	}
	text, ok := delta["text"].(string)
	if !ok {
		return ev
	}
	ev.isText = true
	ev.dataObj = obj
	ev.textPath = []string{"delta", "text"}
	ev.text = text
	if len(preData) > 0 {
		ev.preDataLn = bytes.Join(preData, []byte("\n"))
		if len(ev.preDataLn) > 0 {
			ev.preDataLn = append(ev.preDataLn, '\n')
		}
	}
	return ev
}

// emit returns the wire bytes for this event. If we rewrote text we re-encode
// the JSON; otherwise we pass through the original raw bytes.
func (ev *sseEvent) emit() []byte {
	if !ev.isText || ev.dataObj == nil {
		return ev.raw
	}
	// Walk the JSON path and set the new text.
	cursor := any(ev.dataObj)
	for i, key := range ev.textPath {
		m, ok := cursor.(map[string]any)
		if !ok {
			return ev.raw
		}
		if i == len(ev.textPath)-1 {
			m[key] = ev.text
		} else {
			cursor = m[key]
		}
	}
	encoded, err := json.Marshal(ev.dataObj)
	if err != nil {
		return ev.raw
	}
	var out []byte
	out = append(out, ev.preDataLn...)
	out = append(out, []byte("data: ")...)
	out = append(out, encoded...)
	out = append(out, []byte("\n\n")...)
	return out
}

// virtualBuffer concatenates the text contribution of every event in order.
func virtualBuffer(events []*sseEvent) string {
	var sb strings.Builder
	for _, ev := range events {
		if ev.isText {
			sb.WriteString(ev.text)
		}
	}
	return sb.String()
}

// rewriteSurrogates scans the virtual buffer for any known surrogate and
// rewrites the affected events so the assembled text contains the original
// instead. Earlier contributing events get empty text; the last contributing
// event gets `original + (any tail that was after the surrogate in that
// event)`. The first contributing event keeps any prefix that came before
// the surrogate.
func rewriteSurrogates(events []*sseEvent, sess sseSession) {
	for {
		virtual := virtualBuffer(events)
		var (
			best       string
			bestIdx    = -1
			origMapper = sess.Restore
		)
		for _, sur := range sess.Surrogates() {
			if i := strings.Index(virtual, sur); i >= 0 && (bestIdx == -1 || i < bestIdx) {
				best = sur
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			return
		}
		original := origMapper(best) // session.Restore on the surrogate alone returns the original
		// Find contributing events.
		var (
			cumulative   int
			firstIdx     = -1
			lastIdx      = -1
			firstOffset  int // offset of surrogate start within events[firstIdx].text
			lastOffset   int // offset of surrogate end within events[lastIdx].text
			surrogateEnd = bestIdx + len(best)
		)
		for i, ev := range events {
			if !ev.isText {
				continue
			}
			start := cumulative
			end := cumulative + len(ev.text)
			if firstIdx == -1 && bestIdx >= start && bestIdx < end {
				firstIdx = i
				firstOffset = bestIdx - start
			}
			if surrogateEnd > start && surrogateEnd <= end {
				lastIdx = i
				lastOffset = surrogateEnd - start
			}
			cumulative = end
		}
		if firstIdx == -1 || lastIdx == -1 {
			return // shouldn't happen if Index was found
		}
		// Rewrite.
		if firstIdx == lastIdx {
			ev := events[firstIdx]
			ev.text = ev.text[:firstOffset] + original + ev.text[lastOffset:]
		} else {
			prefix := events[firstIdx].text[:firstOffset]
			tail := events[lastIdx].text[lastOffset:]
			events[firstIdx].text = prefix
			for i := firstIdx + 1; i < lastIdx; i++ {
				if events[i].isText {
					events[i].text = ""
				}
			}
			events[lastIdx].text = original + tail
		}
		// Loop to find more surrogates.
	}
}
