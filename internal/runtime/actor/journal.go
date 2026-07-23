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
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/eventstream"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	"github.com/tochemey/goakt-mcp/internal/runtime"
	"github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
)

// journaler is the Journal Actor.
//
// Journaler receives asynchronous audit events off the critical request path
// and writes them to a durable audit sink. It ensures that request outcomes,
// policy decisions, and lifecycle transitions are recorded without blocking
// the actors that produce those events.
//
// Spawn: GatewayManager spawns Journaler in spawnFoundationalActors via
// ctx.Spawn(ActorNameJournal, newJournalActor(auditSink)) as a child of GatewayManager.
// The audit sink is created from config (createAuditSink).
//
// Relocation: No. Journaler runs on the local node as a child of GatewayManager
// and does not relocate in cluster mode.
//
// All fields are unexported to enforce actor immutability rules.
type journaler struct {
	sink   mcp.AuditSink
	stream eventstream.Stream
	logger goaktlog.Logger

	// writeCh and writerDone implement the AuditOverflowDropNewest policy:
	// sink writes run on a dedicated goroutine fed by this bounded queue so
	// a slow or wedged sink cannot back up the journal mailbox and stall the
	// actors producing audit events. Nil under the default block policy.
	writeCh    chan *mcp.AuditEvent
	writerDone chan struct{}
	// dropped counts events discarded because the writer queue was full.
	// Only touched from Receive, which the actor runtime serializes.
	dropped uint64
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

	if cfg.Audit.OverflowPolicy == mcp.AuditOverflowDropNewest && x.sink != nil {
		queueSize := cfg.Audit.MailboxSize
		if queueSize <= 0 {
			queueSize = mcp.DefaultAuditMailboxSize
		}
		x.writeCh = make(chan *mcp.AuditEvent, queueSize)
		x.writerDone = make(chan struct{})
		go x.runWriter()
	}

	x.logger.Infof("actor=%s starting", naming.ActorNameJournal)
	return nil
}

// runWriter drains the write queue onto the sink. It runs on its own
// goroutine so a slow sink delays only queued audit writes, never the
// journal mailbox. Exits when writeCh is closed in PostStop.
func (x *journaler) runWriter() {
	defer close(x.writerDone)
	for event := range x.writeCh {
		if err := x.sink.Write(event); err != nil {
			x.logger.Warnf("actor=%s audit write failed: %v", naming.ActorNameJournal, err)
		}
	}
}

// Receive handles messages delivered to Journaler.
func (x *journaler) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.logger.Infof("actor=%s started", naming.ActorNameJournal)
	case *runtime.RecordAuditEvent:
		x.handleRecordAuditEvent(msg)
	default:
		ctx.Unhandled()
	}
}

// writerDrainTimeout bounds how long PostStop waits for the drop-newest
// writer goroutine to drain its queue. A wedged sink must not stall gateway
// shutdown; whatever is still queued after the timeout is abandoned.
const writerDrainTimeout = 2 * time.Second

// PostStop performs cleanup after Journaler has stopped.
func (x *journaler) PostStop(ctx *goaktactor.Context) error {
	if x.writeCh != nil {
		close(x.writeCh)
		select {
		case <-x.writerDone:
		case <-time.After(writerDrainTimeout):
			x.logger.Warnf("actor=%s audit writer did not drain within %s; abandoning queued events", naming.ActorNameJournal, writerDrainTimeout)
		}
	}
	if x.sink != nil {
		_ = x.sink.Close()
	}
	x.logger.Infof("actor=%s stopped", naming.ActorNameJournal)
	return nil
}

// handleRecordAuditEvent writes the audit event to the configured sink and
// publishes it on the audit event stream for any external subscribers. Nil
// events are silently ignored. Write failures are logged but do not affect
// the calling actor (journal is off the critical path). Stream publishing
// is non-blocking.
//
// Under the default block policy the write happens inline: a full journal
// mailbox then backpressures producers. Under AuditOverflowDropNewest the
// event is handed to the writer queue without blocking, and dropped (with a
// sampled warning) when the queue is full.
func (x *journaler) handleRecordAuditEvent(msg *runtime.RecordAuditEvent) {
	if msg.Event == nil {
		return
	}

	switch {
	case x.writeCh != nil:
		select {
		case x.writeCh <- msg.Event:
		default:
			x.dropped++
			// Sample the warning so a wedged sink does not flood the log:
			// log the first drop and then every 100th.
			if x.dropped == 1 || x.dropped%100 == 0 {
				x.logger.Warnf("actor=%s audit writer queue full; dropped %d event(s) so far", naming.ActorNameJournal, x.dropped)
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
