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

package mcp_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

func TestErrorCodeConstants(t *testing.T) {
	assert.Equal(t, mcp.ErrorCode("TOOL_NOT_FOUND"), mcp.ErrCodeToolNotFound)
	assert.Equal(t, mcp.ErrorCode("TOOL_UNAVAILABLE"), mcp.ErrCodeToolUnavailable)
	assert.Equal(t, mcp.ErrorCode("TOOL_DISABLED"), mcp.ErrCodeToolDisabled)
	assert.Equal(t, mcp.ErrorCode("SESSION_NOT_FOUND"), mcp.ErrCodeSessionNotFound)
	assert.Equal(t, mcp.ErrorCode("SESSION_UNAVAILABLE"), mcp.ErrCodeSessionUnavailable)
	assert.Equal(t, mcp.ErrorCode("POLICY_DENIED"), mcp.ErrCodePolicyDenied)
	assert.Equal(t, mcp.ErrorCode("QUOTA_EXCEEDED"), mcp.ErrCodeQuotaExceeded)
	assert.Equal(t, mcp.ErrorCode("RATE_LIMITED"), mcp.ErrCodeRateLimited)
	assert.Equal(t, mcp.ErrorCode("CONCURRENCY_LIMIT_REACHED"), mcp.ErrCodeConcurrencyLimitReached)
	assert.Equal(t, mcp.ErrorCode("CREDENTIAL_UNAVAILABLE"), mcp.ErrCodeCredentialUnavailable)
	assert.Equal(t, mcp.ErrorCode("INVOCATION_TIMEOUT"), mcp.ErrCodeInvocationTimeout)
	assert.Equal(t, mcp.ErrorCode("TRANSPORT_FAILURE"), mcp.ErrCodeTransportFailure)
	assert.Equal(t, mcp.ErrorCode("INVALID_REQUEST"), mcp.ErrCodeInvalidRequest)
	assert.Equal(t, mcp.ErrorCode("INTERNAL"), mcp.ErrCodeInternal)
}

func TestNewRuntimeError(t *testing.T) {
	err := mcp.NewRuntimeError(mcp.ErrCodeToolNotFound, "tool not found")
	require.NotNil(t, err)
	assert.Equal(t, mcp.ErrCodeToolNotFound, err.Code)
	assert.Equal(t, "tool not found", err.Message)
	assert.Nil(t, err.Cause)
}

func TestRuntimeError_Error(t *testing.T) {
	t.Run("without cause", func(t *testing.T) {
		err := mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, "access denied")
		assert.Equal(t, "[POLICY_DENIED] access denied", err.Error())
	})
	t.Run("with cause", func(t *testing.T) {
		cause := fmt.Errorf("connection refused")
		err := mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "transport disconnected", cause)
		assert.Equal(t, "[TRANSPORT_FAILURE] transport disconnected: connection refused", err.Error())
	})
}

func TestWrapRuntimeError(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "transport failed", cause)
	require.NotNil(t, err)
	assert.Equal(t, mcp.ErrCodeTransportFailure, err.Code)
	assert.Equal(t, "transport failed", err.Message)
	assert.Equal(t, cause, err.Cause)
}

func TestRuntimeError_Unwrap(t *testing.T) {
	sentinel := fmt.Errorf("sentinel")
	wrapped := mcp.WrapRuntimeError(mcp.ErrCodeInternal, "something went wrong", sentinel)
	assert.True(t, errors.Is(wrapped, sentinel))
}

func TestErrToolNotFound(t *testing.T) {
	assert.Error(t, mcp.ErrToolNotFound)
	var rErr *mcp.RuntimeError
	require.True(t, errors.As(mcp.ErrToolNotFound, &rErr))
	assert.Equal(t, mcp.ErrCodeToolNotFound, rErr.Code)
}
