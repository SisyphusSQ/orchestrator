package metadata

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/openark/orchestrator/internal/observability"
)

const writeConcurrency = 20

var writeSlots = make(chan struct{}, writeConcurrency)

// ExecuteWrite runs a metadata write within the shared concurrency and observability boundary.
func ExecuteWrite(ctx context.Context, write func() error) (resultErr error) {
	ctx, span := observability.StartSpan(ctx, "backend.write_task")
	started := time.Now()
	select {
	case writeSlots <- struct{}{}:
	case <-ctx.Done():
		observability.RecordBackendWrite(ctx, "failure", time.Since(started), 0)
		observability.EndSpan(span, ctx.Err())
		return ctx.Err()
	}
	wait := time.Since(started)
	execution := time.Now()
	result := "failure"
	defer func() {
		recovered := recover()
		observability.RecordBackendWrite(ctx, result, wait, time.Since(execution))
		<-writeSlots
		if recovered != nil {
			observability.EndSpan(span, fmt.Errorf("write task panicked"))
			if _, ok := recovered.(runtime.Error); ok {
				panic(recovered)
			}
			// Preserve the established non-runtime panic contract.
			if _, ok := recovered.(string); !ok {
				_ = recovered.(error)
			}
			return
		}
		observability.EndSpan(span, resultErr)
	}()
	resultErr = write()
	result = observability.Result(resultErr)
	return resultErr
}
