// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package tracing

import (
	"context"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"os"
	"quesma/concurrent"
	"testing"
	"time"
)

func initializeLogger(asyncQueryHook *AsyncTraceLogger) zerolog.Logger {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().
		Timestamp().
		Logger()

	logger = logger.Hook(asyncQueryHook)
	return logger
}

func TestAsyncTraceLogger_OneTransactionWithError(t *testing.T) {
	asyncQueryHook := AsyncTraceLogger{AsyncQueryTrace: concurrent.NewMap[string, TraceCtx]()}
	logger := initializeLogger(&asyncQueryHook)
	ctx := context.WithValue(context.Background(), AsyncIdCtxKey, "quesma_async_search_id_1")
	logger.Info().Ctx(ctx).Msg("Start async search")
	logger.Info().Ctx(ctx).Msg("Continue async search")
	assert.Equal(t, 1, asyncQueryHook.AsyncQueryTrace.Size())
	if traceCtx, ok := asyncQueryHook.AsyncQueryTrace.Load("quesma_async_search_id_1"); ok {
		assert.Equal(t, 2, len(traceCtx.Messages))
	}
	logger.Error().Ctx(ctx).Msg("Error in async search")
	assert.Equal(t, asyncQueryHook.AsyncQueryTrace.Size(), 0)
}

func TestAsyncTraceLogger_OneTransactionOk(t *testing.T) {
	asyncQueryHook := AsyncTraceLogger{AsyncQueryTrace: concurrent.NewMap[string, TraceCtx]()}
	logger := initializeLogger(&asyncQueryHook)
	ctx := context.WithValue(context.Background(), AsyncIdCtxKey, "quesma_async_search_id_1")
	logger.Info().Ctx(ctx).Msg("Start async search")
	logger.Info().Ctx(ctx).Msg("Continue async search")
	assert.Equal(t, 1, asyncQueryHook.AsyncQueryTrace.Size())
	if traceCtx, ok := asyncQueryHook.AsyncQueryTrace.Load("quesma_async_search_id_1"); ok {
		assert.Equal(t, 2, len(traceCtx.Messages))
	}
	ctx = context.WithValue(ctx, TraceEndCtxKey, true)
	logger.Info().Ctx(ctx).Msg("Successful async search")
	assert.Equal(t, asyncQueryHook.AsyncQueryTrace.Size(), 0)
}
