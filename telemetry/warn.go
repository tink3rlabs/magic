package telemetry

import (
	"log/slog"
	"sync"
)

var warnOnceSeen sync.Map

// WarnOnce logs a slog warning exactly once per key for the
// lifetime of the process. It is intended for surfacing soft
// misconfigurations (for example a non-contextual storage adapter
// missing trace spans) without repeatedly spamming logs.
//
// The key is the deduplication key; identical keys after the first
// call are silently dropped. args are passed through to slog as
// structured attributes.
func WarnOnce(key, message string, args ...any) {
	if _, loaded := warnOnceSeen.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	slog.Warn(message, args...)
}

// resetWarnOnceForTest clears the one-shot cache so tests can
// exercise WarnOnce behaviour deterministically. Not part of the
// public API.
func resetWarnOnceForTest() {
	warnOnceSeen = sync.Map{}
}
