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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
)

func TestDroppingMailbox(t *testing.T) {
	t.Run("rejects instead of blocking when full", func(t *testing.T) {
		m := newDroppingMailbox(2)
		require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		assert.ErrorIs(t, m.Enqueue(&goaktactor.ReceiveContext{}), errMailboxFull)
		assert.Equal(t, int64(2), m.Len())
	})

	t.Run("preserves FIFO order", func(t *testing.T) {
		m := newDroppingMailbox(4)
		first := &goaktactor.ReceiveContext{}
		second := &goaktactor.ReceiveContext{}
		require.NoError(t, m.Enqueue(first))
		require.NoError(t, m.Enqueue(second))
		assert.Same(t, first, m.Dequeue())
		assert.Same(t, second, m.Dequeue())
		assert.Nil(t, m.Dequeue())
		assert.True(t, m.IsEmpty())
	})

	t.Run("frees capacity after dequeue", func(t *testing.T) {
		m := newDroppingMailbox(1)
		require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		require.NotNil(t, m.Dequeue())
		assert.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
	})

	t.Run("survives ring wraparound", func(t *testing.T) {
		m := newDroppingMailbox(2)
		for i := 0; i < 100; i++ {
			msg := &goaktactor.ReceiveContext{}
			require.NoError(t, m.Enqueue(msg))
			assert.Same(t, msg, m.Dequeue())
		}
		assert.True(t, m.IsEmpty())
	})

	t.Run("rejects after dispose", func(t *testing.T) {
		m := newDroppingMailbox(2)
		require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		m.Dispose()
		assert.True(t, m.IsEmpty())
		assert.ErrorIs(t, m.Enqueue(&goaktactor.ReceiveContext{}), errMailboxDisposed)
	})

	t.Run("capacity rounds up to a power of two", func(t *testing.T) {
		m := newDroppingMailbox(3)
		for i := 0; i < 4; i++ {
			require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		}
		assert.ErrorIs(t, m.Enqueue(&goaktactor.ReceiveContext{}), errMailboxFull)
	})

	t.Run("non-positive capacity is clamped to the two-slot minimum", func(t *testing.T) {
		m := newDroppingMailbox(0)
		require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		require.NoError(t, m.Enqueue(&goaktactor.ReceiveContext{}))
		assert.ErrorIs(t, m.Enqueue(&goaktactor.ReceiveContext{}), errMailboxFull)
	})
}

// TestDroppingMailbox_ConcurrentProducers hammers the mailbox with parallel
// producers against a single consumer and verifies the MPSC invariants: no
// message is lost silently (delivered + dropped == sent), none is delivered
// twice, and the run is race-clean under -race.
func TestDroppingMailbox_ConcurrentProducers(t *testing.T) {
	const (
		producers   = 8
		perProducer = 5000
	)

	m := newDroppingMailbox(64)
	sent := int64(producers * perProducer)

	var dropped atomic.Int64
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				if err := m.Enqueue(&goaktactor.ReceiveContext{}); err != nil {
					dropped.Add(1)
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	seen := make(map[*goaktactor.ReceiveContext]struct{})
	delivered := int64(0)
	producersDone := false
	for {
		msg := m.Dequeue()
		if msg != nil {
			if _, dup := seen[msg]; dup {
				t.Fatal("message delivered twice")
			}
			seen[msg] = struct{}{}
			delivered++
			continue
		}
		if producersDone && m.IsEmpty() {
			break
		}
		select {
		case <-done:
			producersDone = true
		default:
		}
	}

	require.Equal(t, sent, delivered+dropped.Load(), "delivered + dropped must equal sent")
}
