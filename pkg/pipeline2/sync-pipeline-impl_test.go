/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 */

package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyncPipeline_DoSync(t *testing.T) {
	t.Run("Should return unhandled error", func(t *testing.T) {
		pipeline := NewSyncPipeline(context.Background(), "my-pipeline",
			WireFunc[any]("apply-name", opName),
			WireFunc[any]("fail-here", opError),
			WireFunc[any]("passthrough-error", opSex),
		)
		defer pipeline.Close()

		err := pipeline.SendSync(newTestWork())

		require.NotNil(t, err)
		require.Equal(t, "test failure", err.Error())
		perr, cast := err.(IErrorPipeline)
		require.Equal(t, "fail-here", perr.GetOpName())
		require.Equal(t, "doSync", perr.GetPlace())
		require.True(t, cast)
		require.NotNil(t, perr.GetWork())
	})
	t.Run("Should catch and rethrow error", func(t *testing.T) {
		pipeline := NewSyncPipeline(context.Background(), "my-pipeline",
			WireFunc[any]("apply-name", opName),
			WireFunc[any]("fail-here", opError),
			WireSyncOperator("catch-and-rethrow", mockSyncOp().
				catch(func(err error, work interface{}, context IWorkpieceContext) (newErr error) {
					work.(testwork).slots["error"] = err
					work.(testwork).slots["error-ctx"] = context
					return errors.New("rethrown")
				}).
				create()),
		)
		defer pipeline.Close()

		err := pipeline.SendSync(newTestWork())
		require.NotNil(t, err)
		perr := err.(IErrorPipeline)
		require.Equal(t, "nested error 'rethrown' while handling 'test failure'", perr.Error())
		require.Equal(t, "catch-and-rethrow", perr.GetOpName())
		require.Equal(t, "catch-onErr", perr.GetPlace())
	})
	t.Run("Should return error on ctx termination", func(t *testing.T) {
		ctx := &testContext{}
		pipeline := NewSyncPipeline(ctx, "my-pipeline",
			WireFunc[any]("apply-name", opName))

		testerr := errors.New("context termination")
		ctx.err = testerr
		err := pipeline.DoSync(ctx, nil)

		require.Equal(t, testerr, err)
	})
	t.Run("Should handle not a workpiece with noop operator", func(t *testing.T) {
		type notAWorkpiece struct{}
		ctx := &testContext{}
		v := &notAWorkpiece{}
		pipeline := NewSyncPipeline(ctx, "my-pipeline",
			WireSyncOperator("noop", &NOOP{}))

		require.Nil(t, pipeline.DoSync(ctx, v))
	})
}

func TestSyncPipeline_Close(t *testing.T) {
	pipeline := &SyncPipeline[any]{
		stdin:  make(chan interface{}, 1),
		stdout: make(chan interface{}, 1),
	}
	pipeline.stdout <- newTestWork()
	close(pipeline.stdout)

	require.NotPanics(t, func() {
		pipeline.Close()
	})
}

func Test_checkSyncOperator(t *testing.T) {
	t.Run("Should not panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			checkSyncOperator[any](WireFunc[any]("operator", opAge))
		})
	})

	t.Run("Should panic when operator isn't sync", func(t *testing.T) {
		require.PanicsWithValue(t, "sync pipeline only supports sync operators", func() {
			checkSyncOperator[IWorkpiece](WireAsyncOperator("operator", NewAsyncOp(nil)))
		})
	})
}
