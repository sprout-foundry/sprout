# Bug: PUT /api/settings?layer=workspace Silently Rejects Provider Fields

**Priority:** Medium  
**Component:** `pkg/webui/settings_api_general.go`  
**Discovered:** 2026-04-30

## Summary

`PUT /api/settings?layer=workspace` with provider/model fields returns 200 OK but silently ignores them. The response includes `"warnings":["Unknown fields ignored: [provider model]"]`. Without `?layer=workspace`, `last_used_provider` works correctly.

## Fix

Either: (1) accept provider fields at workspace layer, (2) return 400 for unsupported fields, or (3) document field support per layer.

## Workaround

Omit `?layer=workspace` for provider/model configuration.
