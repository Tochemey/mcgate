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
	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/eventstream"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	"github.com/tochemey/portcullis/internal/runtime/actor/extension"
)

// journaler is the Journal Actor.
//
// Journaler receives asynchronous audit events off the critical request path
// and writes them to a durable audit sink. It ensures that request outcomes,
// policy decisions, and lifecycle transitions are recorded without blocking
// the actors that produce those events.
//
// Spawn: GatewayManager spawns Journaler in spawnFoundationalActors via
// ctx.Spawn(ActorNameJournal, newJournaler()) as a child of GatewayManager.
// The audit sink is created from config (createAuditSink).
//
// Overflow policy: under the default AuditOverflowBlock, the journaler writes
// to the sink inline; its bounded mailbox then backpressures producers. Under
// AuditOverflowDropNewest, sink writes are delegated to a journalWriter child
// actor behind a droppingMailbox: forwarding never blocks, a slow sink delays
// only the writer, and overflow is shed as dead letters (visible through the
// dead-letter log line and the portcullis.actor.dead_letter metric).
//
// Relocation: No. Journaler runs on the local node as a child of GatewayManager
// and does not relocate in cluster mode.
//
// All fields are unexported to enforce actor immutability rules.
type journaler struct {
	sink   mcp.AuditSink
	stream eventstream.Stream
	logger goaktlog.Logger

	// writer is the journalWriter child PID under AuditOverflowDropNewest;
	// nil under the default block policy.
	writer *goaktactor.PID
	// writerQueueSize is the droppingMailbox capacity for the writer child.
	writerQueueSize int
	// dropNewest records the configured overflow policy.
	dropNewest bool
	// droppedWhileDown counts events discarded because the writer child was
	// not running (mid-restart, or stopped after exhausting its restart
	// budget). Actor state, mutated only from Receive.
	droppedWhileDown uint64
}

var _ goaktactor.Actor = (*journaler)(nil)

// newJournaler creates a new Journaler instance
func newJournaler() *journaler {
	return &journaler{}
}

// PreStart initializes Journaler before message processing begins.
func (x *journaler) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()

	cfg := ctx.Extension(extension.ConfigExtensionID).(*extension.ConfigExtension).Config()
	x.sink = createAuditSink(cfg.Audit)

	if streamExt, ok := ctx.Extension(extension.AuditStreamExtensionID).(*extension.AuditStreamExtension); ok && streamExt != nil {
		x.stream = streamExt.Stream()
	}

	x.dropNewest = cfg.Audit.OverflowPolicy == mcp.AuditOverflowDropNewest
	x.writerQueueSize = cfg.Audit.MailboxSize
	if x.writerQueueSize <= 0 {
		x.writerQueueSize = mcp.DefaultAuditMailboxSize
	}

	x.logger.Infof("actor=%s starting", naming.ActorNameJournal)
	return nil
}

// Receive handles messages delivered to Journaler.
func (x *journaler) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		if x.dropNewest && x.sink != nil {
			// Restart on fault (a panicking sink included): the writer holds
			// only a sink reference, and the shared retry budget keeps a
			// deterministically-faulting sink from restarting forever. Once
			// the budget is exhausted the writer stops and the journaler
			// drops events (counted, sampled log) instead of writing inline.
			x.writer = ctx.Spawn(naming.ActorNameJournalWriter, newJournalWriter(x.sink),
				goaktactor.WithMailbox(newDroppingMailbox(x.writerQueueSize)),
				goaktactor.WithSupervisor(restartStrategy()),
				goaktactor.WithLongLived())
		}
		x.logger.Infof("actor=%s started", naming.ActorNameJournal)
	case *runtime.RecordAuditEvent:
		x.handleRecordAuditEvent(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after Journaler has stopped. The writer child (if
// any) is stopped by the actor system before this hook runs, so closing the
// sink here cannot race with an in-flight write.
func (x *journaler) PostStop(ctx *goaktactor.Context) error {
	if x.sink != nil {
		_ = x.sink.Close()
	}
	x.logger.Infof("actor=%s stopped", naming.ActorNameJournal)
	return nil
}

// handleRecordAuditEvent persists the audit event and publishes it on the
// audit event stream for any external subscribers. Nil events are silently
// ignored. Write failures are logged but do not affect the calling actor
// (journal is off the critical path). Stream publishing is non-blocking.
//
// Under the default block policy the write happens inline: a full journal
// mailbox then backpressures producers. Under AuditOverflowDropNewest the
// event is forwarded to the writer child, whose dropping mailbox sheds the
// overflow as dead letters instead of blocking. Drop-newest never falls back
// to inline writes: when the writer is down (mid-restart, or stopped after
// exhausting its restart budget) the event is dropped — writing inline would
// reintroduce the blocking the policy opts out of and expose the journaler
// itself to a faulting sink.
func (x *journaler) handleRecordAuditEvent(ctx *goaktactor.ReceiveContext, msg *runtime.RecordAuditEvent) {
	if msg.Event == nil {
		return
	}

	switch {
	case x.dropNewest && x.sink != nil:
		if x.writer != nil && x.writer.IsRunning() {
			ctx.Tell(x.writer, msg)
		} else {
			x.droppedWhileDown++
			// Sampled so a permanently-stopped writer does not flood the log.
			if x.droppedWhileDown == 1 || x.droppedWhileDown%100 == 0 {
				x.logger.Warnf("actor=%s audit writer is not running; dropped %d event(s) so far", naming.ActorNameJournal, x.droppedWhileDown)
			}
		}
	case x.sink != nil:
		if err := x.sink.Write(msg.Event); err != nil {
			x.logger.Warnf("actor=%s audit write failed: %v", naming.ActorNameJournal, err)
		}
	}

	if x.stream != nil {
		x.stream.Publish(extension.AuditStreamTopic, msg.Event)
	}
}

// journalWriter is the JournalWriter Actor.
//
// It performs the actual sink writes for the journaler under the
// AuditOverflowDropNewest policy. Isolating writes in a child actor keeps the
// journaler responsive when the sink is slow or wedged: the journaler's Tell
// enqueues on the writer's droppingMailbox and returns immediately, and the
// mailbox sheds overflow instead of propagating backpressure.
//
// Spawn: Journaler spawns JournalWriter at PostStart as its child, with a
// droppingMailbox sized by AuditConfig.MailboxSize. The sink is owned (and
// closed) by the journaler.
type journalWriter struct {
	sink   mcp.AuditSink
	logger goaktlog.Logger
}

var _ goaktactor.Actor = (*journalWriter)(nil)

// newJournalWriter creates a JournalWriter that persists to the given sink.
func newJournalWriter(sink mcp.AuditSink) *journalWriter {
	return &journalWriter{sink: sink}
}

// PreStart initializes the logger.
func (x *journalWriter) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	return nil
}

// Receive persists audit events. Failures are logged, never propagated: the
// journal path is off the critical request path.
func (x *journalWriter) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
	case *runtime.RecordAuditEvent:
		if msg.Event == nil {
			return
		}
		if err := x.sink.Write(msg.Event); err != nil {
			x.logger.Warnf("actor=%s audit write failed: %v", naming.ActorNameJournalWriter, err)
		}
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after JournalWriter has stopped. The sink is
// owned by the journaler, so nothing to release here.
func (x *journalWriter) PostStop(ctx *goaktactor.Context) error {
	return nil
}
