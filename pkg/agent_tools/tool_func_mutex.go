package tools

import "sync"

// ToolFuncMu guards the package-level function pointers (RunSubagentFunc,
// ListChangesFunc, etc.) that are written by wireAgentToolFuncs during
// agent construction and read by the handler Execute methods. Without it,
// concurrent agent construction races on these shared vars.
//
// Writers (wireAgentToolFuncs) take Lock; readers (handler Execute
// methods) take RLock.
var ToolFuncMu sync.RWMutex
