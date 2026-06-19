package registry

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewReplacementNativeSessionInSamePanePreservesHistory(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	t1 := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)

	firstReport := reviewReport(StateRunning)
	firstReport.SessionID = "native-a"
	firstReport.CWD = "/work/a"
	firstReport.Tmux = reviewTmuxContext("%1")
	firstReport.ObservedAt = t1
	_, err := store.Report(context.Background(), firstReport)
	if err != nil {
		t.Fatal(err)
	}
	secondReport := reviewReport(StateRunning)
	secondReport.SessionID = "native-b"
	secondReport.CWD = "/work/b"
	secondReport.Tmux = reviewTmuxContext("%1")
	secondReport.ObservedAt = t2
	_, err = store.Report(context.Background(), secondReport)
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := store.List(context.Background(), reviewFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d records: %#v; want old native-a retained as exited and native-b running", len(sessions), sessions)
	}
}

func TestReviewStaleReportDoesNotOverwriteCurrentMetadata(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	newer := time.Date(2026, 6, 19, 10, 2, 0, 0, time.UTC)
	older := newer.Add(-time.Minute)

	newerReport := reviewReport(StateWaiting)
	newerReport.SessionID = "native-a"
	newerReport.ResumeCommand = []string{"codex", "resume", "new"}
	newerReport.CWD = "/work/new"
	newerReport.PID = 200
	newerReport.Tmux = reviewTmuxContext("%2")
	newerReport.Source = "new-source"
	newerReport.Attributes = map[string]string{"phase": "new"}
	newerReport.ObservedAt = newer
	_, err := store.Report(context.Background(), newerReport)
	if err != nil {
		t.Fatal(err)
	}
	olderReport := reviewReport(StateRunning)
	olderReport.SessionID = "native-a"
	olderReport.ResumeCommand = []string{"codex", "resume", "old"}
	olderReport.CWD = "/work/old"
	olderReport.PID = 100
	olderReport.Tmux = reviewTmuxContext("%1")
	olderReport.Source = "old-source"
	olderReport.Attributes = map[string]string{"phase": "old"}
	olderReport.ObservedAt = older
	got, err := store.Report(context.Background(), olderReport)
	if err != nil {
		t.Fatal(err)
	}

	if got.State != StateWaiting {
		t.Fatalf("state = %q, want waiting", got.State)
	}
	if got.CWD != "/work/new" || got.PID != 200 || got.Tmux.PaneID != "%2" ||
		got.Source != "new-source" || got.Attributes["phase"] != "new" || got.ResumeCommand[2] != "new" {
		t.Fatalf("stale event overwrote current fields: %#v", got)
	}
}

func TestReviewFutureObservedAtDoesNotFreezeSession(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	realNow := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	future := realNow.AddDate(10, 0, 0)

	futureReport := reviewReport(StateRunning)
	futureReport.SessionID = "native-a"
	futureReport.CWD = "/wrong/future"
	futureReport.ObservedAt = future
	_, err := store.Report(context.Background(), futureReport)
	if err != nil {
		t.Fatal(err)
	}

	currentReport := reviewReport(StateExited)
	currentReport.SessionID = "native-a"
	currentReport.CWD = "/correct/current"
	currentReport.ObservedAt = realNow
	got, err := store.Report(context.Background(), currentReport)
	if err != nil {
		t.Fatal(err)
	}

	if got.State != StateExited {
		t.Fatalf("state = %q, want exited; future timestamp pinned it until %s", got.State, got.StateChangedAt)
	}
}

func TestReviewSummaryReconcilesPaneBeforeFiltering(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	base := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	old := reviewSession("old-codex", HarnessCodex, StateRunning, base)
	current := reviewSession("new-claude", HarnessClaude, StateRunning, base.Add(time.Minute))
	writeTestSnapshot(t, store, old, current)

	filter := reviewFilter()
	filter.Harness = HarnessCodex
	summaries, err := store.SummaryByTmuxSession(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %#v", summaries)
	}
	if summaries[0].Total != 0 || summaries[0].Exited != 1 {
		t.Fatalf("filtered stale pane occupant counted live: %#v", summaries[0])
	}
}

func TestReviewWeakReportDoesNotDowngradeCanonicalID(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "sessions.json"))
	t1 := time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)
	strongReport := reviewReport(StateRunning)
	strongReport.SessionID = "native-a"
	strongReport.Tmux = reviewTmuxContext("%1")
	strongReport.ObservedAt = t1
	strong, err := store.Report(context.Background(), strongReport)
	if err != nil {
		t.Fatal(err)
	}
	weakReport := reviewReport(StateRunning)
	weakReport.Tmux = reviewTmuxContext("%1")
	weakReport.ObservedAt = t1.Add(time.Minute)
	weak, err := store.Report(context.Background(), weakReport)
	if err != nil {
		t.Fatal(err)
	}
	if weak.ID != strong.ID {
		t.Fatalf("weak pane scan rekeyed strong native identity: %q -> %q", strong.ID, weak.ID)
	}
}

func reviewReport(state State) Report {
	return Report{
		Harness:          HarnessCodex,
		State:            state,
		SessionID:        "",
		SessionPath:      "",
		ResumeCommand:    nil,
		CWD:              "",
		ProjectRoot:      "",
		PID:              0,
		ProcessStartTime: "",
		PPID:             0,
		TTY:              "",
		Tmux:             emptyTmuxContext(),
		Source:           "",
		Confidence:       "",
		Event:            "",
		Attributes:       nil,
		RawPayload:       nil,
		ObservedAt:       time.Time{},
	}
}

func reviewTmuxContext(paneID string) TmuxContext {
	return TmuxContext{
		Inside:          true,
		ServerSocket:    "",
		SessionID:       "$1",
		SessionName:     "dev",
		WindowID:        "",
		WindowIndex:     "",
		WindowName:      "",
		PaneID:          paneID,
		PaneIndex:       "",
		PaneCurrentPath: "",
		PanePID:         0,
		PaneTTY:         "",
		ClientTTY:       "",
	}
}

func reviewSession(id string, harness Harness, state State, observedAt time.Time) Session {
	return Session{
		ID:               id,
		Harness:          harness,
		State:            state,
		SessionID:        "",
		SessionPath:      "",
		ResumeCommand:    nil,
		CWD:              "",
		ProjectRoot:      "",
		PID:              0,
		ProcessStartTime: "",
		PPID:             0,
		TTY:              "",
		Tmux:             reviewTmuxContext("%1"),
		Source:           "",
		Confidence:       "",
		LastEvent:        "",
		Attributes:       nil,
		RawPayload:       nil,
		CreatedAt:        observedAt,
		UpdatedAt:        observedAt,
		LastSeenAt:       observedAt,
		LastObservedAt:   observedAt,
		StateChangedAt:   observedAt,
		LastEventAt:      time.Time{},
		EndedAt:          time.Time{},
	}
}

func reviewFilter() Filter {
	return Filter{
		Harness:     "",
		State:       "",
		TmuxSession: "",
		ActiveOnly:  false,
	}
}
