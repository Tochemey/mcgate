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
	"errors"
	"sync/atomic"

	goaktactor "github.com/tochemey/goakt/v4/actor"
)

// errMailboxFull is returned by droppingMailbox.Enqueue when the mailbox is at
// capacity. GoAkt logs the error and routes the message to dead letters, so
// drops surface through the existing dead-letter log line and the
// mcgate.actor.dead_letter metric.
var errMailboxFull = errors.New("mailbox is full: message dropped")

// errMailboxDisposed is returned by droppingMailbox.Enqueue after Dispose.
var errMailboxDisposed = errors.New("mailbox is disposed")

// cacheLinePad separates the producer and consumer cursors so they never
// share a cache line (false sharing would make every Enqueue invalidate the
// consumer's line and vice versa).
type cacheLinePad [64]byte

// droppingCell is one slot of the ring. sequence implements the Vyukov
// bounded-queue protocol: it equals the slot index when the cell is free for
// the producer at that index, and index+1 once a message is published for
// the consumer.
type droppingCell struct {
	sequence atomic.Uint64
	message  *goaktactor.ReceiveContext
}

// droppingMailbox is a lock-free, bounded MPSC mailbox that rejects new
// messages when full instead of blocking the sender. It backs actors whose
// work may be shed under pressure (the audit journal writer): producers stay
// non-blocking, and overflow becomes a dead letter rather than backpressure.
//
// Design (Vyukov bounded queue, MPSC specialization):
//   - The ring is one flat array allocated at construction and reused for
//     the mailbox's lifetime: no per-message nodes, so steady-state
//     operation allocates nothing and adds no GC pressure.
//   - Producers claim slots with a single CAS on enqueuePos; publication is
//     an atomic store of the cell sequence. No locks anywhere.
//   - The single consumer (the actor's dispatcher, per the goakt mailbox
//     contract) advances dequeuePos with plain atomic stores and nils the
//     message slot on dequeue so consumed messages become collectible
//     immediately.
//   - Capacity is rounded up to the next power of two so slot indexing is a
//     mask instead of a modulo; a request for 1000 yields 1024 usable slots.
//
// FIFO ordering is preserved. Dequeue is non-blocking and returns nil when
// empty, matching goakt's built-in mailboxes.
type droppingMailbox struct {
	cells    []droppingCell
	mask     uint64
	disposed atomic.Bool

	_          cacheLinePad
	enqueuePos atomic.Uint64
	_          cacheLinePad
	dequeuePos atomic.Uint64
}

// enforce the goakt contract at compile time.
var _ goaktactor.Mailbox = (*droppingMailbox)(nil)

// newDroppingMailbox creates a dropping mailbox with at least the given
// capacity (rounded up to a power of two). The minimum is two slots: with a
// single slot the sequence protocol cannot distinguish "published at
// position N" from "freed for position N+1", and a producer would overwrite
// an unconsumed message.
func newDroppingMailbox(capacity int) *droppingMailbox {
	size := uint64(2)
	for int(size) < capacity {
		size <<= 1
	}

	m := &droppingMailbox{
		cells: make([]droppingCell, size),
		mask:  size - 1,
	}

	for i := range m.cells {
		m.cells[i].sequence.Store(uint64(i))
	}
	return m
}

// Enqueue inserts the message, or returns errMailboxFull when at capacity
// (errMailboxDisposed after Dispose). It never blocks: the only loop retries
// a CAS lost to another producer, never waits on the consumer.
func (m *droppingMailbox) Enqueue(msg *goaktactor.ReceiveContext) error {
	if m.disposed.Load() {
		return errMailboxDisposed
	}

	for {
		pos := m.enqueuePos.Load()
		cell := &m.cells[pos&m.mask]
		seq := cell.sequence.Load()

		switch {
		case seq == pos:
			// The slot is free for this position; claim it.
			if m.enqueuePos.CompareAndSwap(pos, pos+1) {
				cell.message = msg
				cell.sequence.Store(pos + 1)
				return nil
			}
			// Another producer claimed pos first; retry at the new position.
		case seq < pos:
			// The slot still holds an unconsumed message from a full lap ago:
			// the ring is full.
			return errMailboxFull
		default:
			// A producer that claimed an earlier position for this slot has
			// published meanwhile; reload and retry.
		}
	}
}

// Dequeue removes and returns the oldest message, or nil when empty (or when
// the oldest slot's producer has claimed but not yet published — the message
// becomes visible on a later pass). Single consumer only, per the goakt
// mailbox contract.
func (m *droppingMailbox) Dequeue() *goaktactor.ReceiveContext {
	pos := m.dequeuePos.Load()
	cell := &m.cells[pos&m.mask]
	if cell.sequence.Load() != pos+1 {
		return nil
	}

	msg := cell.message
	cell.message = nil
	m.dequeuePos.Store(pos + 1)
	// Mark the slot free for the producer one full lap ahead.
	cell.sequence.Store(pos + m.mask + 1)
	return msg
}

// IsEmpty reports whether the mailbox currently has no messages.
// The value is a snapshot and may change immediately under concurrency.
func (m *droppingMailbox) IsEmpty() bool {
	return m.Len() == 0
}

// Len returns the current number of queued messages. The value is a snapshot
// and may change immediately under concurrency.
func (m *droppingMailbox) Len() int64 {
	enq := m.enqueuePos.Load()
	deq := m.dequeuePos.Load()
	if enq <= deq {
		return 0
	}
	return int64(enq - deq)
}

// Dispose rejects all further Enqueue calls and releases queued messages.
// Per the goakt mailbox contract it is called when the owning actor stops,
// after the consumer is done; it must not race with an active Dequeue.
func (m *droppingMailbox) Dispose() {
	m.disposed.Store(true)
	for m.Dequeue() != nil { //nolint:revive // draining releases message references for the GC
	}
}
