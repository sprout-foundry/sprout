//go:build js && wasm

package main

// opfs_replica_recovery.go — the SP-046 sync-recovery path: crash
// recovery (recoverFromBrowserCrash), server-side reconcile
// (handleSyncReconcile), and the persisted-seq-number bookkeeping
// (getPersistedSeqNumbers, initSyncRecovery). Split out of opfs_replica.go.
import (
	"syscall/js"
)

// initSyncRecoveryFunc is called on WASM startup to check for persisted state
// and send a sync_recover message to the server if needed.
//
// Signature: initSyncRecovery(clientID, sendWebSocketFunc): void
func initSyncRecoveryFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		println("[SP-046] initSyncRecovery: expected (clientID, sendWebSocketFunc)")
		return nil
	}
	clientID := args[0].String()
	sendFunc := args[1]

	// Read persisted seq numbers from OPFS metadata
	seqs := getPersistedSeqNumbers()

	if len(seqs) == 0 {
		println("[SP-046] initSyncRecovery: no persisted state, skipping recovery")
		return nil
	}

	println("[SP-046] initSyncRecovery: found", len(seqs), "persisted files, sending sync_recover")

	// Build the sync_recover message
	seqsObj := js.Global().Get("Object").New()
	for path, seq := range seqs {
		seqsObj.Set(path, seq)
	}

	msg := js.Global().Get("Object").New()
	msg.Set("type", "sync_recover")
	data := js.Global().Get("Object").New()
	data.Set("client_id", clientID)
	data.Set("seqs", seqsObj)
	msg.Set("data", data)

	// Send via the provided callback
	sendFunc.Invoke(msg)

	return nil
}

// handleSyncReconcileFunc processes the server's reconciliation plan.
//
// Signature: handleSyncReconcile(planData): void
func handleSyncReconcileFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		println("[SP-046] handleSyncReconcile: expected reconcile plan")
		return nil
	}

	planData := args[0]
	if !planData.Truthy() {
		println("[SP-046] handleSyncReconcile: nil plan data")
		return nil
	}

	// Extract the plan array
	planArr := planData.Get("plan")
	if !planArr.Truthy() {
		println("[SP-046] handleSyncReconcile: no plan array")
		return nil
	}

	length := planArr.Length()
	println("[SP-046] handleSyncReconcile: processing", length, "items")

	for i := 0; i < length; i++ {
		item := planArr.Index(i)
		action := item.Get("action").String()
		filePath := item.Get("file_path").String()

		switch action {
		case "sync_ok":
			// Nothing to do
			println("[SP-046] handleSyncReconcile:", filePath, "is in sync")
		case "container_ahead":
			// Will receive replay patches — handled by handleReplayFile
			println("[SP-046] handleSyncReconcile:", filePath, "needs replay from container")
		case "browser_ahead":
			// Browser has newer data — will push to server
			println("[SP-046] handleSyncReconcile:", filePath, "browser is ahead, will push")
		case "diverged":
			// Conflict — log it, let the UI handle
			println("[SP-046] handleSyncReconcile:", filePath, "DIVERGED - conflict resolution needed")
		default:
			println("[SP-046] handleSyncReconcile: unknown action", action, "for", filePath)
		}
	}

	return nil
}

// getPersistedSeqNumbers reads persisted sequence numbers from OPFS metadata.
func getPersistedSeqNumbers() map[string]int64 {
	// This reads from the OPFS file metadata store that was persisted
	// before the browser crash. The actual OPFS read is done via JS interop.
	seqs := make(map[string]int64)

	// Try to read from the global OPFS metadata store
	opfsMeta := js.Global().Get("opfsMetadata")
	if !opfsMeta.Truthy() {
		return seqs
	}

	keys := js.Global().Get("Object").Call("keys", opfsMeta)
	for i := 0; i < keys.Length(); i++ {
		key := keys.Index(i).String()
		val := opfsMeta.Get(key)
		if seq := val.Get("browserSeq"); seq.Truthy() {
			if seq.Type() == js.TypeNumber {
				seqs[key] = int64(seq.Int())
			}
		}
	}

	return seqs
}

// recoverFromBrowserCrashFunc reads persisted seq numbers from OPFS metadata
// and returns them for recovery processing.
//
// Signature: recoverFromBrowserCrash(): {path: seq, ...}
func recoverFromBrowserCrashFunc(this js.Value, args []js.Value) interface{} {
	seqs := getPersistedSeqNumbers()

	result := js.Global().Get("Object").New()
	for path, seq := range seqs {
		result.Set(path, seq)
	}
	return result
}
