package core

import (
	"sync"
)

// store implements the Store interface
type store struct {
	mu        sync.RWMutex
	state     State
	reducer   Reducer
	listeners []func(State)
}

// NewStore creates a new store with the given reducer and initial state
func NewStore(reducer Reducer, initialState State) Store {
	return &store{
		state:     deepCopyState(initialState),
		reducer:   reducer,
		listeners: make([]func(State), 0),
	}
}

// GetState returns a copy of the current state
func (s *store) GetState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyState(s.state)
}

// Dispatch sends an action to update state
func (s *store) Dispatch(action Action) {
	s.mu.Lock()
	oldState := s.state
	newState := s.reducer(oldState, action)

	// Only update if state actually changed
	if !statesEqual(oldState, newState) {
		s.state = newState
		listeners := make([]func(State), len(s.listeners))
		copy(listeners, s.listeners)
		s.mu.Unlock()

		// Notify listeners outside of lock
		for _, listener := range listeners {
			listener(deepCopyState(newState))
		}
	} else {
		s.mu.Unlock()
	}
}

// Subscribe registers a listener for state changes
func (s *store) Subscribe(listener func(State)) func() {
	s.mu.Lock()
	s.listeners = append(s.listeners, listener)
	index := len(s.listeners) - 1
	s.mu.Unlock()

	// Return unsubscribe function
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Remove listener by swapping with last and truncating
		if index < len(s.listeners) {
			s.listeners[index] = s.listeners[len(s.listeners)-1]
			s.listeners = s.listeners[:len(s.listeners)-1]
		}
	}
}

// Select retrieves a specific part of the state
func (s *store) Select(selector func(State) interface{}) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return selector(s.state)
}

// Helper functions

// deepCopyState creates a deep copy of the state
func deepCopyState(state State) State {
	if state == nil {
		return nil
	}

	newState := make(State)
	for k, v := range state {
		newState[k] = deepCopyValue(v)
	}
	return newState
}

// deepCopyValue creates a deep copy of a value
func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, v := range val {
			newMap[k] = deepCopyValue(v)
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(val))
		for i, v := range val {
			newSlice[i] = deepCopyValue(v)
		}
		return newSlice
	default:
		// For primitive types and others, return as-is
		return val
	}
}

// statesEqual checks if two states are equal
func statesEqual(s1, s2 State) bool {
	if len(s1) != len(s2) {
		return false
	}

	for k, v1 := range s1 {
		v2, ok := s2[k]
		if !ok {
			return false
		}

		// Simple equality check - could be enhanced
		if !valuesEqual(v1, v2) {
			return false
		}
	}

	return true
}

// valuesEqual checks if two values are equal
func valuesEqual(v1, v2 interface{}) bool {
	// Handle special cases
	if v1 == nil && v2 == nil {
		return true
	}
	if v1 == nil || v2 == nil {
		return false
	}

	// Check if both are State types
	s1, ok1 := v1.(State)
	s2, ok2 := v2.(State)
	if ok1 && ok2 {
		return statesEqual(s1, s2)
	}

	// Check if both are maps
	m1, ok1 := v1.(map[string]interface{})
	m2, ok2 := v2.(map[string]interface{})
	if ok1 && ok2 {
		return statesEqual(State(m1), State(m2))
	}

	// For primitive types, use reflection
	switch v1 := v1.(type) {
	case string:
		v2str, ok := v2.(string)
		return ok && v1 == v2str
	case int:
		v2int, ok := v2.(int)
		return ok && v1 == v2int
	case bool:
		v2bool, ok := v2.(bool)
		return ok && v1 == v2bool
	case float64:
		v2float, ok := v2.(float64)
		return ok && v1 == v2float
	default:
		// For slices and other types, just check pointer equality
		// In a production system, you'd want deep equality
		return false
	}
}

// CombineReducers combines multiple reducers into one
func CombineReducers(reducers map[string]Reducer) Reducer {
	return func(state State, action Action) State {
		newState := make(State)
		hasChanged := false

		for key, reducer := range reducers {
			oldSubState, _ := state[key].(State)
			if oldSubState == nil {
				oldSubState = make(State)
			}

			newSubState := reducer(oldSubState, action)

			if !statesEqual(oldSubState, newSubState) {
				hasChanged = true
			}

			newState[key] = newSubState
		}

		// Copy any keys not handled by reducers
		for key, value := range state {
			if _, handled := reducers[key]; !handled {
				newState[key] = value
			}
		}

		if hasChanged {
			return newState
		}
		return state
	}
}
