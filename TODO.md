# WebUI Issues - TODO

## Critical Issues

### 1. **Outdated Embedded Static Files** ⚠️ CRITICAL
- **Location**: `pkg/webui/static/js/main.f9741241.js`
- **Problem**: Embedded static files are outdated (from Feb 16 07:31) compared to build files (from Feb 16 11:14)
- **Impact**: WebSocket connection fails because embedded files don't have correct `/ws` path
- **Root Cause**: The embedded file has 0 occurrences of "/ws" while the source code has it
- **Fix Required**: Rebuild and redeploy webui to `pkg/webui/static/`

### 2. **WebSocket Connection Failed** ⚠️ CRITICAL
- **Error**: `WebSocket connection to 'ws://localhost:54321/' failed: Error during WebSocket handshake: Unexpected response code: 200`
- **Root Cause**: Frontend trying to connect to root `/` instead of `/ws` endpoint due to outdated embedded files
- **Impact**: All real-time features broken (stats updates, event streaming, etc.)
- **Fix**: Deploy updated build to pkg/webui/static/

### 3. **Terminal History API Returns 400 Error** ⚠️ HIGH
- **Error**: `Failed to get terminal history: Error: HTTP error! status: 400`
- **Message**: "Session ID is required"
- **Location**: `pkg/webui/api.go:389-391`
- **Problem**: Frontend calling `/api/terminal/history` without `session_id` parameter
- **Root Cause**: Frontend (`api.ts:114`) tries to fetch without having a session ID first
- **Fix Required**: 
  - Option 1: Make session_id optional in backend
  - Option 2: Frontend should get sessions list first, then fetch history
  - Option 3: Return empty history when no session_id provided

### 4. **Missing Manifest.json** - 404 Error ⚠️ MEDIUM
- **Error**: `Manifest fetch from http://localhost:54321/manifest.json failed, code 404`
- **Impact**: PWA functionality broken, app not installable
- **Location**: `pkg/webui/server.go:108-110` tries to serve manifest.json
- **Fix Required**: Build/deploy webui properly to include manifest.json in static files

## UI/UX Issues

### 5. **Disconnected Status Display**
- **Problem**: UI shows "Disconnected" status prominently
- **Expected**: Should show proper connection status or error message
- **Impact**: User confusion about application state

### 6. **All Navigation Buttons Disabled** ⚠️ HIGH
- **Problem**: Chat, Editor, Git, Logs view buttons all show "disabled" attribute
- **Impact**: Cannot navigate to different sections
- **Root Cause**: Likely due to no active connection/session

### 7. **No Recent Files Displayed**
- **Problem**: "📁 RECENT FILES (0)" shows "No files"
- **Expected**: Should show recently accessed files
- **Impact**: Feature not functional

### 8. **No Chat Activity**
- **Problem**: "📋 CHAT ACTIVITY" shows "No activity yet"
- **Expected**: Should show chat history/queries
- **Impact**: Cannot see past interactions

## Functional Issues

### 9. **Provider/Model Dropdowns Disabled**
- **Problem**: Provider and Model dropdowns show "disabled" attribute
- **Impact**: Cannot change AI provider or model from UI
- **Expected**: Should be editable for configuration

### 10. **Send Button Disabled**
- **Problem**: Send button disabled even with text in input
- **Impact**: Cannot send queries to AI
- **Root Cause**: Likely due to no active WebSocket connection

## Build/Deployment Issues

### 11. **Build and Static Files Out of Sync**
- **Problem**: `webui/build/` has newer files than `pkg/webui/static/`
- **Root Cause**: Build completed but not deployed to embedded location
- **Fix Required**: Run deployment step to copy build to pkg/webui/static/

### 12. **Missing Icons**
- **404 Errors**: Various icon files may be missing
- **Files**: icon-192.png, icon-512.png, favicon.ico
- **Impact**: Browser tab shows default icon, PWA icons missing

## Performance Issues

### 13. **Repeated API Polling Failures**
- **Problem**: Multiple repeated requests to `/api/terminal/history` every few seconds
- **Impact**: Wasted network requests, console spam
- **Root Cause**: Frontend continues polling even after errors

## Testing Required

After fixes:
1. Test WebSocket connection establishes properly
2. Test all navigation buttons work
3. Test terminal history loads correctly
4. Test provider/model configuration works
5. Test sending queries to AI
6. Test file browsing and editing
7. Test Git operations
8. Test PWA installation (manifest.json)
9. Test all icons load correctly
10. Test real-time stats updates

## Priority Fix Order

1. **FIRST**: Rebuild and deploy webui to fix WebSocket and embedded files
2. **SECOND**: Fix terminal history API to handle no session ID case
3. **THIRD**: Verify all features work with connection established
4. **FOURTH**: UI/UX improvements for disabled states
5. **FIFTH**: Add missing icons and manifest
