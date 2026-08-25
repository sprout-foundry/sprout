package embedding

import "context"

// maxConcurrentInference caps concurrent ORT Run calls process-wide.
// 2 rather than 1 so an interactive query isn't stuck behind a background build,
// and rather than NumCPU because ORT already fans matmul across IntraOpNumThreads.
const maxConcurrentInference = 2

// inferenceGate is the process-wide inference permit pool.
var inferenceGate = make(chan struct{}, maxConcurrentInference)

// acquireInference blocks until a permit is available or ctx is done.
func acquireInference(ctx context.Context) (release func(), err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case inferenceGate <- struct{}{}:
		return func() { <-inferenceGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
