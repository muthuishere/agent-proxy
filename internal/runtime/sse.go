package runtime

import (
	"bytes"
	"io"

	"github.com/muthuishere/agent-proxy/internal/codec"
)

// sseSession is the subset of vault.Session used by the streaming reader.
type sseSession interface {
	codec.Restorer
	Surrogates() []string
}

// sseRestoreReader wraps an SSE response body and restores vault tokens
// across the byte stream — including when a surrogate is split across
// multiple SSE events (e.g. Anthropic content_block_delta deltas).
//
// Algorithm:
//
//   - Buffer raw bytes until we have one or more complete events
//     (delimited by "\n\n").
//   - Parse each event. JSON-shaped text_delta events expose their `delta.text`
//     for rewriting; other events stay opaque.
//   - Maintain a logical concatenation of all pending text_delta texts (the
//     "virtual buffer"). Run surrogate detection on it. When found, rewrite
//     contributing events to embed the original (last contributor gets the
//     full original; earlier contributors get their text emptied).
//   - Emit events whose contribution is *outside* the holdback window (the
//     length of the longest known surrogate). Anything still inside the
//     window stays buffered until more data arrives or EOF is reached.
//
// Why: a surrogate can be 100+ chars long. Anthropic streams content_block
// deltas as small fragments. If a surrogate spans multiple deltas, naive
// per-event Restore (the previous design) never sees it as a contiguous
// substring. The 2026-04-25 production bug observed `sk-ldme-...` reach
// OpenAI as a Bearer token because of exactly this. See
// docs/specs/spec-sse-delta-aware-restore.md.
type sseRestoreReader struct {
	src     io.ReadCloser
	raw     []byte       // bytes received but not yet split into events
	pending []*sseEvent  // parsed events held back pending surrogate completion
	ready   []byte       // bytes emitted to the caller's next Read
	sess    sseSession
}

func newSSERestoreReader(src io.ReadCloser, sess sseSession) io.ReadCloser {
	return &sseRestoreReader{src: src, sess: sess}
}

func (r *sseRestoreReader) Read(p []byte) (int, error) {
	if len(r.ready) > 0 {
		n := copy(p, r.ready)
		r.ready = r.ready[n:]
		return n, nil
	}

	tmp := make([]byte, 4096)
	n, readErr := r.src.Read(tmp)
	if n > 0 {
		r.raw = append(r.raw, tmp[:n]...)
	}
	atEOF := readErr != nil

	// Split off complete events from raw.
	for {
		idx := bytes.Index(r.raw, []byte("\n\n"))
		if idx == -1 {
			break
		}
		eventBytes := r.raw[:idx]
		r.raw = r.raw[idx+2:]
		ev := parseSSEEvent(eventBytes)
		// Reattach the trailing \n\n on the raw bytes so emit() can pass
		// non-text events through unchanged.
		ev.raw = append(ev.raw, '\n', '\n')
		r.pending = append(r.pending, ev)
	}

	// At EOF flush any partial event (no trailing \n\n) as opaque.
	if atEOF && len(r.raw) > 0 {
		r.pending = append(r.pending, &sseEvent{raw: append([]byte(nil), r.raw...)})
		r.raw = nil
	}

	// Run surrogate rewrite over all currently-pending events.
	rewriteSurrogates(r.pending, r.sess)

	// Decide which pending events are safe to emit.
	holdback := r.maxSurrogateLen()
	emitUpTo := r.safeEmitIndex(holdback, atEOF)
	for i := 0; i < emitUpTo; i++ {
		ev := r.pending[i]
		out := ev.emit()
		// After Restore on individual events we still want to handle encoded
		// blobs that survived as a whole-string token in the data: line.
		out = []byte(codec.RestoreEncodedBlobs(string(out), r.sess, codec.EncodedBlobOptions{Strategy: codec.EmbeddedTokens}))
		r.ready = append(r.ready, out...)
	}
	r.pending = r.pending[emitUpTo:]

	if atEOF {
		// Flush whatever remains.
		for _, ev := range r.pending {
			out := ev.emit()
			out = []byte(codec.RestoreEncodedBlobs(string(out), r.sess, codec.EncodedBlobOptions{Strategy: codec.EmbeddedTokens}))
			r.ready = append(r.ready, out...)
		}
		r.pending = nil
	}

	if len(r.ready) > 0 {
		k := copy(p, r.ready)
		r.ready = r.ready[k:]
		return k, nil
	}
	return 0, readErr
}

// maxSurrogateLen returns the longest known surrogate length in the session.
// This is the holdback window — we cannot emit text-delta events whose
// contribution is within the last `maxSurrogateLen` bytes of the virtual
// buffer because a surrogate could still complete with a not-yet-arrived
// chunk.
func (r *sseRestoreReader) maxSurrogateLen() int {
	max := 0
	for _, s := range r.sess.Surrogates() {
		if len(s) > max {
			max = len(s)
		}
	}
	return max
}

// safeEmitIndex returns the number of leading pending events that can be
// emitted now. Non-text events are always safe to emit if all preceding
// text events are safe. A text event is safe if its contribution ends at
// least `holdback` bytes before the end of the virtual buffer.
func (r *sseRestoreReader) safeEmitIndex(holdback int, atEOF bool) int {
	if atEOF || holdback == 0 {
		return len(r.pending)
	}
	virtual := virtualBuffer(r.pending)
	cumulative := 0
	safeIdx := 0
	for i, ev := range r.pending {
		if ev.isText {
			cumulative += len(ev.text)
			if cumulative+holdback <= len(virtual) {
				safeIdx = i + 1
			} else {
				break
			}
		} else {
			// Non-text event: safe iff all earlier text events were safe.
			if safeIdx == i {
				safeIdx = i + 1
			}
		}
	}
	return safeIdx
}

func (r *sseRestoreReader) Close() error {
	return r.src.Close()
}
