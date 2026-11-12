package engine

import (
	"path/filepath"
	"strings"
	"time"
)

// DoneEvent describes a finished file or torrent. It is delivered to the
// handler installed with SetDoneHandler, on its own goroutine.
type DoneEvent struct {
	Type      string // "file" or "torrent"
	InfoHash  string
	Path      string // file path within the torrent, or the torrent name
	Size      int64
	StartedAt time.Time
	FileCount int
}

// SetDoneHandler installs the function called when a file or torrent finishes.
func (e *Engine) SetDoneHandler(h func(DoneEvent)) {
	e.mut.Lock()
	e.onDone = h
	e.mut.Unlock()
}

// trackDoneLocked emits completion events for an admitted torrent. An item only
// counts as finished after it was seen incomplete in this process, so torrents
// that are already complete when loaded (restarts, restored sessions) do not
// re-trigger the handler.
func (e *Engine) trackDoneLocked(model *Torrent, complete bool) {
	if model.startedAt.IsZero() {
		model.startedAt = e.nowLocked()
	}
	emit := func(ev DoneEvent) {
		if h := e.onDone; h != nil && safeDonePath(ev.Path) {
			go h(ev)
		}
	}
	for _, file := range model.Files {
		if !model.selection[file.Path] || file.Size <= 0 {
			continue
		}
		if file.Percent < 100 {
			file.seenIncomplete = true
		} else if file.seenIncomplete && !file.doneNotified {
			file.doneNotified = true
			emit(DoneEvent{Type: "file", InfoHash: model.InfoHash, Path: file.Path, Size: file.Size, StartedAt: model.startedAt, FileCount: len(model.Files)})
		}
	}
	if !complete {
		model.seenIncomplete = true
	} else if model.seenIncomplete && !model.doneNotified {
		model.doneNotified = true
		emit(DoneEvent{Type: "torrent", InfoHash: model.InfoHash, Path: model.Name, Size: model.Size, StartedAt: model.startedAt, FileCount: len(model.Files)})
	}
}

// safeDonePath rejects torrent-supplied names that could escape the download
// directory when a done command joins them to it.
func safeDonePath(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return false
	}
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	return true
}
