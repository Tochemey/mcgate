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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	actorextension "github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/audit"
)

const (
	grainTestTenantID mcp.TenantID = "tenant-grain"
	grainTestClientID mcp.ClientID = "client-grain"
)

// activateSessionGrain activates a session grain for the given tool using
// an executor produced by the supplied ExecutorFactory. The grain is
// activated directly through the grain engine with a SessionDependency,
// exactly as the router does; the journal, registrar, and tool supervisor
// are running so lifecycle notifications reach a real supervisor.
func activateSessionGrain(t *testing.T, tool mcp.Tool, executor mcp.ToolExecutor) (goaktactor.ActorSystem, *goaktactor.GrainIdentity, func()) {
	t.Helper()
	ctx := context.Background()

	cfg := testConfig()
	cfg.Audit.Sink = audit.NewMemorySink()

	factoryExt := actorextension.NewExecutorFactoryExtension(&mockExecutorFactory{executor: executor})

	system, err := goaktactor.NewActorSystem("test-mcgate",
		goaktactor.WithLogger(goaktlog.DiscardLogger),
		goaktactor.WithExtensions(
			actorextension.NewConfigExtension(cfg),
			factoryExt,
		),
	)
	require.NoError(t, err)
	require.NoError(t, system.Start(ctx))

	spawnToolRuntimeForTest(t, ctx, system, tool)

	sessDep := actorextension.NewSessionDependency(grainTestTenantID, grainTestClientID, tool.ID, tool, nil)
	identity, err := goaktactor.GrainOf[*sessionGrain](
		ctx,
		system,
		naming.SessionName(grainTestTenantID, grainTestClientID, tool.ID),
		goaktactor.WithGrainDependencies(sessDep),
		goaktactor.WithGrainDeactivateAfter(sessionIdleTimeout(tool)),
	)
	require.NoError(t, err)
	require.NotNil(t, identity)

	stop := func() {
		require.NoError(t, system.Stop(ctx))
	}
	return system, identity, stop
}

func TestSessionGrain_InvokeReturnsSuccess(t *testing.T) {
	tool := validStdioTool("grain-invoke-tool")
	system, identity, stop := activateSessionGrain(t, tool, &mockExecutor{
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{mcp.OutputKeyContent: "ok"},
		},
	})
	defer stop()

	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvoke{
		Invocation: &mcp.Invocation{
			ToolID: tool.ID,
			Method: mcp.MethodToolsCall,
			Correlation: mcp.CorrelationMeta{
				TenantID: grainTestTenantID,
				ClientID: grainTestClientID,
			},
		},
	}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeResult)
	require.True(t, ok)
	require.NoError(t, result.Err)
	require.NotNil(t, result.Result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
}

func TestSessionGrain_InvokeRejectsToolMismatch(t *testing.T) {
	tool := validStdioTool("grain-mismatch-tool")
	system, identity, stop := activateSessionGrain(t, tool, &mockExecutor{})
	defer stop()

	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvoke{
		Invocation: &mcp.Invocation{
			ToolID: "wrong-tool",
			Method: mcp.MethodToolsCall,
			Correlation: mcp.CorrelationMeta{
				TenantID: grainTestTenantID,
				ClientID: grainTestClientID,
			},
		},
	}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeResult)
	require.True(t, ok)
	require.Error(t, result.Err)
}

func TestSessionGrain_InvokeRejectsNilInvocation(t *testing.T) {
	tool := validStdioTool("grain-nil-tool")
	system, identity, stop := activateSessionGrain(t, tool, &mockExecutor{})
	defer stop()

	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvoke{}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeResult)
	require.True(t, ok)
	require.Error(t, result.Err)
}

func TestSessionGrain_InvokeReportsFailureToSupervisor(t *testing.T) {
	tool := validStdioTool("grain-failure-tool")
	failing := &mockExecutor{
		err: errors.New("transport exploded"),
	}
	system, identity, stop := activateSessionGrain(t, tool, failing)
	defer stop()

	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvoke{
		Invocation: &mcp.Invocation{
			ToolID: tool.ID,
			Method: mcp.MethodToolsCall,
			Correlation: mcp.CorrelationMeta{
				TenantID: grainTestTenantID,
				ClientID: grainTestClientID,
			},
		},
	}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeResult)
	require.True(t, ok)
	require.NotNil(t, result.Result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Result.Status)
}

func TestSessionGrain_InvokeStreamFallsBackToSyncExecute(t *testing.T) {
	tool := validStdioTool("grain-stream-fallback-tool")
	system, identity, stop := activateSessionGrain(t, tool, &mockExecutor{
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{},
		},
	})
	defer stop()

	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvokeStream{
		Invocation: &mcp.Invocation{
			ToolID: tool.ID,
			Method: mcp.MethodToolsCall,
			Correlation: mcp.CorrelationMeta{
				TenantID: grainTestTenantID,
				ClientID: grainTestClientID,
			},
		},
	}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeStreamResult)
	require.True(t, ok)
	require.Nil(t, result.StreamResult, "non-stream executor must not return a StreamResult")
	require.NotNil(t, result.Result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
}

func TestSessionGrain_InvokeStreamDeliversProgressAndFinal(t *testing.T) {
	tool := validStdioTool("grain-stream-delivery-tool")

	progressCh := make(chan mcp.ProgressEvent, 2)
	progressCh <- mcp.ProgressEvent{Progress: 0.5, Message: "halfway"}
	progressCh <- mcp.ProgressEvent{Progress: 1.0, Message: "done"}
	close(progressCh)

	finalCh := make(chan *mcp.ExecutionResult, 1)
	finalCh <- &mcp.ExecutionResult{
		Status: mcp.ExecutionStatusSuccess,
		Output: map[string]any{mcp.OutputKeyContent: "streamed"},
	}
	close(finalCh)

	streamExec := &mockStreamExecutor{
		streamResult: &mcp.StreamingResult{
			Progress: progressCh,
			Final:    finalCh,
		},
	}
	streamExec.result = &mcp.ExecutionResult{Status: mcp.ExecutionStatusSuccess}

	system, identity, stop := activateSessionGrain(t, tool, streamExec)
	defer stop()

	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvokeStream{
		Invocation: &mcp.Invocation{
			ToolID: tool.ID,
			Method: mcp.MethodToolsCall,
			Correlation: mcp.CorrelationMeta{
				TenantID: grainTestTenantID,
				ClientID: grainTestClientID,
			},
		},
	}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeStreamResult)
	require.True(t, ok)
	require.NotNil(t, result.StreamResult, "ToolStreamExecutor must produce a StreamingResult")

	progressSeen := drainStreamProgress(t, result.StreamResult.Progress, 2, 500*time.Millisecond)
	assert.Len(t, progressSeen, 2)

	final := drainStreamFinal(t, result.StreamResult.Final, 500*time.Millisecond)
	require.NotNil(t, final)
	assert.Equal(t, mcp.ExecutionStatusSuccess, final.Status)
}

func TestSessionGrain_InvokeStreamCrossNodeFallsBackToSync(t *testing.T) {
	tool := validStdioTool("grain-stream-crossnode-tool")

	progressCh := make(chan mcp.ProgressEvent)
	close(progressCh)
	finalCh := make(chan *mcp.ExecutionResult, 1)
	finalCh <- &mcp.ExecutionResult{Status: mcp.ExecutionStatusSuccess}
	close(finalCh)

	streamExec := &mockStreamExecutor{
		streamResult: &mcp.StreamingResult{Progress: progressCh, Final: finalCh},
	}
	streamExec.result = &mcp.ExecutionResult{
		Status: mcp.ExecutionStatusSuccess,
		Output: map[string]any{mcp.OutputKeyContent: "sync"},
	}

	system, identity, stop := activateSessionGrain(t, tool, streamExec)
	defer stop()

	// A CallerNode that does not match this node forces the synchronous
	// fallback: a StreamingResult's channels cannot cross a node boundary.
	resp, err := system.AskGrain(context.Background(), identity, &runtime.SessionInvokeStream{
		Invocation: &mcp.Invocation{
			ToolID: tool.ID,
			Method: mcp.MethodToolsCall,
			Correlation: mcp.CorrelationMeta{
				TenantID: grainTestTenantID,
				ClientID: grainTestClientID,
			},
		},
		CallerNode: "remote-node:19999",
	}, askTimeout)
	require.NoError(t, err)

	result, ok := resp.(*runtime.SessionInvokeStreamResult)
	require.True(t, ok)
	require.Nil(t, result.StreamResult, "cross-node stream request must not return channels")
	require.NotNil(t, result.Result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
}

// drainStreamProgress collects up to expectedCount progress events from the
// channel, failing the test when the deadline expires before that many
// events arrive.
func drainStreamProgress(t *testing.T, progress <-chan mcp.ProgressEvent, expectedCount int, timeout time.Duration) []mcp.ProgressEvent {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	events := make([]mcp.ProgressEvent, 0, expectedCount)
	for len(events) < expectedCount {
		select {
		case evt, ok := <-progress:
			if !ok {
				return events
			}
			events = append(events, evt)
		case <-deadline.C:
			t.Fatalf("expected %d progress events within %s, got %d", expectedCount, timeout, len(events))
			return events
		}
	}
	return events
}

// drainStreamFinal reads the terminal ExecutionResult from the channel,
// failing the test if the deadline expires first.
func drainStreamFinal(t *testing.T, final <-chan *mcp.ExecutionResult, timeout time.Duration) *mcp.ExecutionResult {
	t.Helper()

	select {
	case result := <-final:
		return result
	case <-time.After(timeout):
		t.Fatalf("no final ExecutionResult within %s", timeout)
		return nil
	}
}
