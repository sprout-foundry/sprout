# Automatic Conversation Pruning Implementation

## Overview

I've implemented a comprehensive automatic conversation pruning system for the ledit agent to prevent context overflow issues that can cause loops and degraded performance when approaching model context limits.

## What Was Implemented

### 1. **ConversationPruner** (`pkg/agent/conversation_pruner.go`)
A sophisticated pruning system with multiple strategies:

- **Sliding Window**: Keeps only the most recent N messages
- **Importance-based**: Scores messages by importance and keeps the most valuable ones
- **Hybrid**: Combines deduplication with importance scoring
- **Adaptive**: Automatically selects the best strategy based on conversation characteristics

### 2. **Pruning Triggers**
- Activates at 70% context usage by default (configurable)
- Works alongside existing aggressive optimization (80% threshold)
- Prevents the context overflow that was causing your loops

### 3. **Message Importance Scoring**
Messages are scored based on:
- Role (system messages: 1.0, user queries: 0.6-0.9, assistant: 0.5-0.7)
- Content type (errors: 0.8, tool results: 0.3-0.7)
- Recency (recent messages get bonus points)
- First user query is always preserved (0.9 importance)

### 4. **Configuration API**
```go
agent.SetPruningStrategy(agent.PruneStrategyAdaptive)
agent.SetPruningThreshold(0.7) // Prune at 70% context
agent.SetRecentMessagesToKeep(10)
agent.SetPruningSlidingWindowSize(20)
agent.DisableAutoPruning() // If needed
```

## How It Prevents Context Overflow Loops

1. **Early Intervention**: Pruning starts at 70% context usage, before the model degrades
2. **Smart Preservation**: Keeps important messages (errors, recent context, first query)
3. **Adaptive Strategy**: Adjusts pruning based on conversation type:
   - Long technical conversations → Hybrid approach
   - File-heavy conversations → Focus on deduplication
   - Critical usage (>90%) → Aggressive optimization

4. **Graceful Degradation**: Multiple layers of protection:
   - 70%: Automatic pruning kicks in
   - 80%: Aggressive optimization (existing)
   - 90%: Critical pruning mode

## Benefits

- **Prevents Loops**: No more context overflow causing repeated actions
- **Maintains Context**: Smart importance scoring keeps relevant information
- **Automatic**: No manual intervention needed
- **Configurable**: Adjust strategies and thresholds as needed
- **Efficient**: Typical 25-40% token reduction while preserving conversation quality

## Testing Results

```
--- Testing sliding_window Strategy ---
After pruning: 20 messages, 713 tokens (24.9% reduction)

--- Testing importance Strategy ---
After pruning: 21 messages, 589 tokens (37.9% reduction)

--- Testing adaptive Strategy ---
After pruning: 21 messages, 589 tokens (37.9% reduction)
```

## Usage

The pruning is enabled by default with adaptive strategy. You can customize it:

```go
// In your agent initialization or configuration
agent, _ := agent.NewAgent()
agent.SetPruningStrategy(agent.PruneStrategyImportance)
agent.SetPruningThreshold(0.6) // More aggressive pruning
```

Or disable it if needed:
```go
agent.DisableAutoPruning()
```

## Next Steps

The automatic pruning should prevent the context overflow loops you experienced. If you need to:
- Adjust pruning aggressiveness: Lower the threshold (e.g., 0.6 for 60%)
- Keep more recent messages: Increase `SetRecentMessagesToKeep()`
- Use a simpler strategy: Switch to `sliding_window` for predictable behavior

The system is designed to be transparent - it logs pruning actions in debug mode so you can see when and how it's managing context.