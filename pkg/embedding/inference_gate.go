package embedding

import "context"

// maxConcurrentInference caps how many ORT Run calls execute at once,
// process-wide.
//
// defaultBatchAttentionBudget bounds the working set of ONE call (~400 MB —
// self-attention materializes a [batch, heads, seq, seq] score tensor). It says
// nothing about how many calls run at once, and the concurrency is not bounded
// by anything else: the ONNX provider is a process-wide singleton whose Embed
// path takes only a read lock, so every caller proceeds in parallel. In the
// daemon that meant one build per agent per chat session all inferencing
// simultaneously — four concurrent builds were observed taking RSS from 1 GB to
// 4.5 GB. The per-call budget multiplied by an unbounded factor is not a budget.
//
// 2 rather than 1 so an interactive query (proactive context runs one on every
// prompt) is not stuck behind a long background build batch, and rather than
// NumCPU because ORT already fans each matmul across IntraOpNumThreads (up to
// 4) — more concurrent sessions add memory, not throughput.
const maxConcurrentInference = 2

// inferenceGate is the process-wide inference permit pool. Buffered channel
// rather than x/sync/semaphore: no new dependency, and select gives ctx-aware
// acquisition for free.
var inferenceGate = make(chan struct{}, maxConcurrentInference)

// acquireInference blocks until a permit is available or ctx is done. The
// returned release function is safe to defer and must be called exactly once
// when acquisition succeeded.
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
