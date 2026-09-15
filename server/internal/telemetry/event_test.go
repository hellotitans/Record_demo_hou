package telemetry

import (
	"testing"
	"time"
)

func TestEventValidate(t *testing.T) {
	valid := func() Event {
		return NewEvent(EventArgumentFeedback, ZoneDecision, "sess-1", "debate-1", nil)
	}

	tests := []struct {
		name    string
		mutate  func(*Event)
		wantErr bool
	}{
		{name: "valid decision event", mutate: func(*Event) {}},
		{name: "valid execution event", mutate: func(e *Event) { e.Zone = ZoneExecution }},
		{name: "missing event name", mutate: func(e *Event) { e.EventName = "" }, wantErr: true},
		{name: "missing zone", mutate: func(e *Event) { e.Zone = "" }, wantErr: true},
		{name: "unknown zone", mutate: func(e *Event) { e.Zone = Zone("ad") }, wantErr: true},
		{name: "missing session id", mutate: func(e *Event) { e.SessionID = "" }, wantErr: true},
		{name: "missing debate id", mutate: func(e *Event) { e.DebateID = "" }, wantErr: true},
		{name: "zero timestamp", mutate: func(e *Event) { e.Timestamp = time.Time{} }, wantErr: true},
		// user_id 允许为空：未登录用户用匿名哈希，强制要求会把大量有效数据挡在门外。
		{name: "empty user id is allowed", mutate: func(e *Event) { e.UserID = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := valid()
			tt.mutate(&ev)
			err := ev.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestNewEventFillsMetadata(t *testing.T) {
	ev := NewEvent(EventRoundRead, ZoneDecision, "sess-1", "debate-1", nil)

	if ev.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", ev.SchemaVersion, SchemaVersion)
	}
	if ev.EventID == "" {
		t.Error("EventID is empty")
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestNewEventIDIsUnique(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := newEventID()
		if id == "" {
			t.Fatal("newEventID() returned empty string")
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate event id %q after %d iterations", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestMustProperties(t *testing.T) {
	props := MustProperties(ArgumentFeedbackProps{
		Round:      3,
		Side:       "data",
		ArgumentID: "r3-data-2",
		Verdict:    VerdictRepetitive,
	})

	if got := props["verdict"]; got != string(VerdictRepetitive) {
		t.Errorf("verdict = %v, want %q", got, VerdictRepetitive)
	}
	if got := props["round"]; got != float64(3) {
		t.Errorf("round = %v, want 3 (JSON numbers decode to float64)", got)
	}
}

// MustProperties 不可序列化时降级为空表而不是 panic —— 埋点永远不该让主流程崩溃。
func TestMustPropertiesDoesNotPanic(t *testing.T) {
	if got := MustProperties(func() {}); got != nil {
		t.Errorf("MustProperties(func) = %v, want nil", got)
	}
}
