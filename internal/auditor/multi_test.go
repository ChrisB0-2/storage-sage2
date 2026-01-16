package auditor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ChrisB0-2/storage-sage/internal/core"
)

// mockAuditor is a test auditor that records events in memory
type mockAuditor struct {
	mu     sync.Mutex
	events []core.AuditEvent
}

func (m *mockAuditor) Record(_ context.Context, evt core.AuditEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
}

func (m *mockAuditor) Events() []core.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]core.AuditEvent{}, m.events...)
}

func TestMulti_Record(t *testing.T) {
	m1 := &mockAuditor{}
	m2 := &mockAuditor{}
	m3 := &mockAuditor{}

	multi := NewMulti(m1, m2, m3)

	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: "test",
		Path:   "/tmp/test.txt",
	}

	multi.Record(context.Background(), evt)

	// All auditors should have received the event
	for i, m := range []*mockAuditor{m1, m2, m3} {
		events := m.Events()
		if len(events) != 1 {
			t.Errorf("auditor %d: expected 1 event, got %d", i, len(events))
		}
		if events[0].Action != "test" {
			t.Errorf("auditor %d: expected action 'test', got %q", i, events[0].Action)
		}
		if events[0].Path != "/tmp/test.txt" {
			t.Errorf("auditor %d: expected path '/tmp/test.txt', got %q", i, events[0].Path)
		}
	}
}

func TestMulti_MultipleRecords(t *testing.T) {
	m1 := &mockAuditor{}
	m2 := &mockAuditor{}

	multi := NewMulti(m1, m2)

	for i := 0; i < 5; i++ {
		evt := core.AuditEvent{
			Time:   time.Now(),
			Level:  "info",
			Action: "test",
			Fields: map[string]any{"index": i},
		}
		multi.Record(context.Background(), evt)
	}

	// Both auditors should have 5 events
	for i, m := range []*mockAuditor{m1, m2} {
		events := m.Events()
		if len(events) != 5 {
			t.Errorf("auditor %d: expected 5 events, got %d", i, len(events))
		}
	}
}

func TestMulti_EmptyAuditors(t *testing.T) {
	// Multi with no auditors should not panic
	multi := NewMulti()

	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "info",
		Action: "test",
	}

	// Should not panic
	multi.Record(context.Background(), evt)
}

func TestMulti_SingleAuditor(t *testing.T) {
	m1 := &mockAuditor{}
	multi := NewMulti(m1)

	evt := core.AuditEvent{
		Time:   time.Now(),
		Level:  "warn",
		Action: "delete",
	}

	multi.Record(context.Background(), evt)

	events := m1.Events()
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestMulti_ConcurrentRecords(t *testing.T) {
	m1 := &mockAuditor{}
	m2 := &mockAuditor{}

	multi := NewMulti(m1, m2)

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				evt := core.AuditEvent{
					Time:   time.Now(),
					Level:  "info",
					Action: "test",
					Fields: map[string]any{"goroutine": id, "iteration": j},
				}
				multi.Record(context.Background(), evt)
			}
		}(i)
	}

	wg.Wait()

	expected := goroutines * iterations

	// Both auditors should have all events
	for i, m := range []*mockAuditor{m1, m2} {
		events := m.Events()
		if len(events) != expected {
			t.Errorf("auditor %d: expected %d events, got %d", i, expected, len(events))
		}
	}
}

func TestMulti_PreservesContext(t *testing.T) {
	// Create a context with a value
	type ctxKey string
	ctx := context.WithValue(context.Background(), ctxKey("test"), "value")

	var receivedCtx context.Context
	ctxCapture := &contextCapturingAuditor{captureFunc: func(c context.Context) {
		receivedCtx = c
	}}

	multi := NewMulti(ctxCapture)
	multi.Record(ctx, core.AuditEvent{Time: time.Now()})

	if receivedCtx == nil {
		t.Fatal("context was not passed to auditor")
	}
	if receivedCtx.Value(ctxKey("test")) != "value" {
		t.Error("context value not preserved")
	}
}

func TestMulti_ImplementsAuditor(t *testing.T) {
	// Verify Multi implements core.Auditor interface
	var _ core.Auditor = (*Multi)(nil)
}

// contextCapturingAuditor captures the context for testing
type contextCapturingAuditor struct {
	captureFunc func(context.Context)
}

func (c *contextCapturingAuditor) Record(ctx context.Context, _ core.AuditEvent) {
	if c.captureFunc != nil {
		c.captureFunc(ctx)
	}
}
