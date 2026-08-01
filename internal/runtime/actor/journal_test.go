// MIT License
//
// Copyright (c) 2026 GoAkt Team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//

package actor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/eventstream"
	"github.com/tochemey/goakt/v4/testkit"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	"github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/audit"
)

func TestJournalActor(t *testing.T) {
	ctx := context.Background()

	t.Run("starts and stops cleanly", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(cfg)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Equal(t, naming.ActorNameJournal, pid.Name())

		waitForActors()
	})

	t.Run("records audit events", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink
		system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(cfg)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		ev := &mcp.AuditEvent{
			Type:     mcp.AuditEventTypeInvocationComplete,
			TenantID: "tenant-1",
			ClientID: "client-1",
			ToolID:   "tool-1",
			Outcome:  "success",
		}
		err = goaktactor.Tell(ctx, pid, &runtime.RecordAuditEvent{Event: ev})
		require.NoError(t, err)
		waitForActors()

		events := sink.Events()
		require.Len(t, events, 1)
		assert.Equal(t, mcp.AuditEventTypeInvocationComplete, events[0].Type)
		assert.Equal(t, "tenant-1", events[0].TenantID)
		assert.Equal(t, "success", events[0].Outcome)
	})

	t.Run("ignores nil event", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		kit.Spawn(ctx, "journal-nil-event", newJournaler())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.Send("journal-nil-event", &runtime.RecordAuditEvent{Event: nil})
		probe.ExpectNoMessage()
		probe.Stop()
	})

	t.Run("handles write failure gracefully", func(t *testing.T) {
		failingSink := &failingAuditSink{}
		cfg := testConfig()
		cfg.Audit.Sink = failingSink
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		kit.Spawn(ctx, "journal-fail", newJournaler())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.Send("journal-fail", &runtime.RecordAuditEvent{
			Event: &mcp.AuditEvent{Type: mcp.AuditEventTypeInvocationComplete, TenantID: "t1"},
		})
		probe.ExpectNoMessage()
		probe.Stop()
	})

	t.Run("unhandles unknown message", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "journal-unknown", newJournaler())
		require.NoError(t, err)
		require.NoError(t, pid.Tell(ctx, pid, "unknown"))
		waitForActors()
	})

	t.Run("publishes events on the audit stream when extension is registered", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink

		stream := eventstream.New()
		t.Cleanup(stream.Close)

		subscriber := stream.AddSubscriber()
		stream.Subscribe(subscriber, extension.AuditStreamTopic)

		system, stop := testActorSystem(t, goaktactor.WithExtensions(
			extension.NewConfigExtension(cfg),
			extension.NewAuditStreamExtension(stream),
		))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		ev := &mcp.AuditEvent{
			Type:     mcp.AuditEventTypeInvocationComplete,
			TenantID: "tenant-1",
			ClientID: "client-1",
			ToolID:   "tool-1",
			Outcome:  "success",
		}
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RecordAuditEvent{Event: ev}))

		payload := drainOneEvent(t, subscriber, 500*time.Millisecond)

		got, ok := payload.(*mcp.AuditEvent)
		require.True(t, ok, "expected *mcp.AuditEvent, got %T", payload)
		assert.Equal(t, ev.Type, got.Type)
		assert.Equal(t, ev.ToolID, got.ToolID)
		assert.Equal(t, ev.Outcome, got.Outcome)
	})

	t.Run("writes to sink without publishing when stream extension is absent", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink

		system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(cfg)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		ev := &mcp.AuditEvent{
			Type:    mcp.AuditEventTypeInvocationComplete,
			ToolID:  "tool-2",
			Outcome: "success",
		}
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RecordAuditEvent{Event: ev}))
		waitForActors()

		require.Len(t, sink.Events(), 1)
		assert.Equal(t, mcp.ToolID("tool-2"), mcp.ToolID(sink.Events()[0].ToolID))
	})
}

// drainOneEvent polls the subscriber every 10ms until a message arrives or the
// timeout expires, then returns the payload of the first message.
func drainOneEvent(t *testing.T, subscriber eventstream.Subscriber, timeout time.Duration) any {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for msg := range subscriber.Iterator() {
			return msg.Payload()
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("no message received within %s", timeout)
	return nil
}

// blockingAuditSink blocks every Write until release is closed, simulating a
// wedged sink (e.g. a stalled filesystem).
type blockingAuditSink struct {
	release chan struct{}
	mu      sync.Mutex
	events  []*mcp.AuditEvent
}

func (b *blockingAuditSink) Write(event *mcp.AuditEvent) error {
	<-b.release
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
	return nil
}

func (b *blockingAuditSink) Close() error { return nil }

func (b *blockingAuditSink) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

func TestJournalActor_DropNewestOverflow(t *testing.T) {
	ctx := context.Background()

	sink := &blockingAuditSink{release: make(chan struct{})}
	cfg := testConfig()
	cfg.Audit.Sink = sink
	cfg.Audit.MailboxSize = 2
	cfg.Audit.OverflowPolicy = mcp.AuditOverflowDropNewest

	system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(cfg)))
	defer stop()

	pid, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
	require.NoError(t, err)
	waitForActors()

	// Flood the journal while the sink is wedged. With the block policy this
	// would eventually stall; with drop_newest the journal keeps consuming
	// its mailbox and drops the overflow.
	for i := 0; i < 10; i++ {
		ev := &mcp.AuditEvent{Type: mcp.AuditEventTypeInvocationComplete, ToolID: "tool-drop", Outcome: "success"}
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RecordAuditEvent{Event: ev}))
	}
	waitForActors()

	// The journal actor must still be responsive despite the wedged sink.
	require.True(t, pid.IsRunning())

	// Un-wedge the sink: only the queued events (bounded by MailboxSize plus
	// the one in-flight write) are persisted; the rest were dropped.
	close(sink.release)
	require.Eventually(t, func() bool { return sink.count() >= 1 }, 2*time.Second, 20*time.Millisecond)
	waitForActors()
	assert.LessOrEqual(t, sink.count(), 3, "overflow events must be dropped, not queued unboundedly")
}

// panicOnceSink panics on the first Write and succeeds afterwards, simulating
// a sink with a transient fault.
type panicOnceSink struct {
	mu       sync.Mutex
	panicked bool
	events   []*mcp.AuditEvent
}

func (p *panicOnceSink) Write(event *mcp.AuditEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.panicked {
		p.panicked = true
		panic("sink transient fault")
	}
	p.events = append(p.events, event)
	return nil
}

func (p *panicOnceSink) Close() error { return nil }

func (p *panicOnceSink) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func TestJournalActor_WriterRestartsAfterSinkPanic(t *testing.T) {
	ctx := context.Background()

	sink := &panicOnceSink{}
	cfg := testConfig()
	cfg.Audit.Sink = sink
	cfg.Audit.OverflowPolicy = mcp.AuditOverflowDropNewest

	system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(cfg)))
	defer stop()

	pid, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
	require.NoError(t, err)
	waitForActors()

	// First event triggers the sink panic; the writer must be restarted by
	// its supervisor rather than stopped for good.
	ev := &mcp.AuditEvent{Type: mcp.AuditEventTypeInvocationComplete, ToolID: "tool-panic", Outcome: "success"}
	require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RecordAuditEvent{Event: ev}))
	waitForActors()

	// Subsequent events must flow again through the restarted writer.
	require.Eventually(t, func() bool {
		_ = goaktactor.Tell(ctx, pid, &runtime.RecordAuditEvent{Event: ev})
		return sink.count() >= 1
	}, 5*time.Second, 50*time.Millisecond, "writer must resume writing after the sink panic")

	require.True(t, pid.IsRunning(), "journaler must survive a writer fault")
}
