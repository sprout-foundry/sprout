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
// 1 rather than 2: two concurrent Run calls on the single shared ONNX session
// deadlock ORT's intra-op thread pool (observed on-device during a background
// index build overlapped with daemon embedding ops — proactive context, turn
// embedding, memory migration, semantic search). Both Run goroutines spin
// forever at ~50% CPU per worker, the build's session.Run never returns, the
// 45-minute BuildTimeout can't cancel it (Run ignores Go contexts), and the
// building flag stays true until the process is killed.
//
// Serializing Run calls makes the deadlock structurally impossible. The cost
// is that an interactive embedding op (e.g. proactive context on every prompt)
// queues behind whatever in-flight Run it overlaps — but acquireInference is
// ctx-aware, so a blocked op fails fast on its own deadline instead of
// hanging. Between build batches the permit is free, so daemon ops still get
// service.
const maxConcurrentInference = 1

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
