package engine

import (
	"sync"
	"testing"
	"time"
)

func collectDone(e *Engine) (func() []DoneEvent, *sync.WaitGroup) {
	var mu sync.Mutex
	var got []DoneEvent
	wg := &sync.WaitGroup{}
	e.SetDoneHandler(func(ev DoneEvent) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
		wg.Done()
	})
	return func() []DoneEvent { mu.Lock(); defer mu.Unlock(); return append([]DoneEvent(nil), got...) }, wg
}

func TestDoneEventsFireOnlyOnObservedCompletion(t *testing.T) {
	e := New()
	events, wg := collectDone(e)
	model := &Torrent{InfoHash: "abc", Name: "show", Size: 30, Files: []*File{{Path: "show/a.mkv", Size: 10, Percent: 100}, {Path: "show/b.mkv", Size: 20, Percent: 40}}, selection: map[string]bool{"show/a.mkv": true, "show/b.mkv": true}}

	// a file that is already complete when first seen must not fire
	e.mut.Lock()
	e.trackDoneLocked(model, false)
	e.mut.Unlock()
	if model.startedAt.IsZero() {
		t.Fatal("startedAt not recorded on first admission")
	}
	if got := events(); len(got) != 0 {
		t.Fatalf("fired for already-complete file: %+v", got)
	}

	// b finishes: one file event, then the torrent completes: one torrent event
	model.Files[1].Percent = 100
	wg.Add(2)
	e.mut.Lock()
	e.trackDoneLocked(model, true)
	e.trackDoneLocked(model, true) // idempotent
	e.mut.Unlock()
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	got := events()
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %+v", got)
	}
	types := map[string]DoneEvent{}
	for _, ev := range got {
		types[ev.Type] = ev
	}
	if types["file"].Path != "show/b.mkv" || types["file"].Size != 20 || types["file"].FileCount != 2 {
		t.Fatalf("bad file event %+v", types["file"])
	}
	if types["torrent"].Path != "show" || types["torrent"].InfoHash != "abc" || types["torrent"].Size != 30 {
		t.Fatalf("bad torrent event %+v", types["torrent"])
	}
}

func TestDoneEventsSkipTorrentsLoadedComplete(t *testing.T) {
	e := New()
	events, _ := collectDone(e)
	model := &Torrent{InfoHash: "abc", Name: "x", Size: 10, Files: []*File{{Path: "x/a", Size: 10, Percent: 100}}, selection: map[string]bool{"x/a": true}}
	e.mut.Lock()
	e.trackDoneLocked(model, true)
	e.mut.Unlock()
	time.Sleep(20 * time.Millisecond)
	if got := events(); len(got) != 0 {
		t.Fatalf("restored complete torrent fired: %+v", got)
	}
}

func TestSafeDonePath(t *testing.T) {
	for p, want := range map[string]bool{"a/b.mkv": true, "name": true, "": false, "/etc/passwd": false, "../x": false, "a/../../x": false, `a\..\x`: false, `\x`: false, "a..b/c": true} {
		if got := safeDonePath(p); got != want {
			t.Errorf("safeDonePath(%q)=%v want %v", p, got, want)
		}
	}
}
