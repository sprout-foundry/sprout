# Textarea Cursor Fix - Summary

## Problem
The textarea input field had a critical issue where:
- Text entered backwards (cursor stayed at position 0)
- Example: Typing "hello" resulted in "olleh"
- Cursor jumped to beginning after each keystroke
- This made the input unusable for normal typing

## Root Cause
React re-renders were interfering with cursor position:

1. **Controlled Component Issues:**
   - Textarea was controlled with `value={currentInput}` prop
   - Every keystroke triggered parent state update via `onChange`
   - Parent re-render caused CommandInput to re-render
   - React's reconciliation process reset cursor position to 0

2. **Auto-Resize Interference:**
   - JavaScript auto-resize logic ran on every input change
   - DOM manipulation during typing caused cursor jumps
   - CSS `field-sizing` property also interfered

## Solution
Made textarea completely uncontrolled:

### 1. Removed Value Prop
```typescript
// Before (controlled):
<textarea
  value={currentInput}
  onChange={handleChange}
  ...
/>

// After (uncontrolled):
<textarea
  ref={inputRef}
  onChange={handleChange}
  ...
/>
```

### 2. Direct DOM Manipulation
```typescript
// Read value directly from ref:
const textareaValue = inputRef.current?.value || '';

// Write value directly to DOM:
if (inputRef.current) {
  inputRef.current.value = '';
}
```

### 3. One-Time Initialization
```typescript
const isInitializedRef = useRef(false);

useEffect(() => {
  // Only set value once on mount, not on every render
  if (!isInitializedRef.current && inputRef.current && value !== undefined) {
    inputRef.current.value = value;
    isInitializedRef.current = true;
  }
}, [value]);
```

### 4. Removed All Auto-Resize JavaScript
- Removed `useEffect` that watched `currentInput` changes
- Removed `field-sizing: content` from CSS
- Let CSS `min-height` and `max-height` handle sizing naturally

## Files Changed

### `/home/alanp/dev/personal/ledit/webui/src/components/CommandInput.tsx`
- Made textarea uncontrolled (removed `value` prop)
- Added one-time initialization logic
- Updated all handlers to use ref instead of state
- Removed auto-resize JavaScript

### `/home/alanp/dev/personal/ledit/webui/src/components/CommandInput.css`
- Removed `field-sizing: content` property
- Removed `transition: height 0.2s ease-out` (was causing glitches)
- Kept `min-height: 44px` and `max-height: 200px` for natural sizing

## Testing Results

✅ Typing works correctly - cursor stays at end
✅ Multiline support (Shift+Enter) works
✅ Clear button works
✅ Send button works
✅ Cursor position preserved during typing
✅ No text reversal
✅ No cursor jumping to position 0

## Key Insight
**Uncontrolled components are essential for text inputs** that need to maintain cursor position during typing. React's controlled component pattern works against the browser's native cursor management, causing the cursor to reset on every re-render.

## Best Practice
For textareas and text inputs where users type normally:
- Use uncontrolled components with `ref`
- Only use controlled components when you need to programmatically control the value
- If you must use controlled components, use `useLayoutEffect` and cursor position restoration
- But preferably, avoid controlled text inputs entirely for typing scenarios
