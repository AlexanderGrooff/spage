package pkg

import (
	"sync"

	"github.com/AlexanderGrooff/spage/pkg/common"
)

// HandlerTracker tracks which handlers have been notified and which have already run
type HandlerTracker struct {
	mu       sync.RWMutex
	notified map[string]bool // Maps handler name to whether it's been notified
	executed map[string]bool // Maps handler name to whether it's been executed
	handlers map[string]Task // Maps handler name to handler task
	hostName string          // Name of the host this tracker is for
}

// NewHandlerTracker creates a new HandlerTracker for the given host and handlers
func NewHandlerTracker(hostName string, handlers []Task) *HandlerTracker {

	ht := &HandlerTracker{
		notified: make(map[string]bool),
		executed: make(map[string]bool),
		handlers: make(map[string]Task),
		hostName: hostName,
	}

	// Index handlers by name
	for _, handler := range handlers {
		ht.handlers[handler.Name] = handler
		common.LogDebug("Added handler to tracker", map[string]interface{}{
			"handler": handler.Name,
			"host":    hostName,
		})
	}

	return ht
}

// NotifyHandler marks a handler as notified
func (ht *HandlerTracker) NotifyHandler(handlerName string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	common.LogDebug("Attempting to notify handler", map[string]interface{}{
		"handler": handlerName,
		"host":    ht.hostName,
		"available_handlers": func() []string {
			var names []string
			for name := range ht.handlers {
				names = append(names, name)
			}
			return names
		}(),
	})

	if _, exists := ht.handlers[handlerName]; !exists {
		common.LogWarn("Handler not found", map[string]interface{}{
			"handler": handlerName,
			"host":    ht.hostName,
		})
		return
	}

	if !ht.notified[handlerName] {
		ht.notified[handlerName] = true
		common.LogDebug("Handler notified", map[string]interface{}{
			"handler": handlerName,
			"host":    ht.hostName,
		})
	}
}

// NotifyHandlers marks multiple handlers as notified
func (ht *HandlerTracker) NotifyHandlers(handlerNames []string) {
	for _, handlerName := range handlerNames {
		ht.NotifyHandler(handlerName)
	}
}

// IsNotified checks if a handler has been notified
func (ht *HandlerTracker) IsNotified(handlerName string) bool {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	return ht.notified[handlerName]
}

// IsExecuted checks if a handler has been executed
func (ht *HandlerTracker) IsExecuted(handlerName string) bool {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	return ht.executed[handlerName]
}

// MarkExecuted marks a handler as executed
func (ht *HandlerTracker) MarkExecuted(handlerName string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	ht.executed[handlerName] = true
	common.LogDebug("Handler marked as executed", map[string]interface{}{
		"handler": handlerName,
		"host":    ht.hostName,
	})
}

// GetNotifiedHandlers returns a list of handlers that have been notified but not yet executed
func (ht *HandlerTracker) GetNotifiedHandlers() []Task {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	var notifiedHandlers []Task
	for handlerName, isNotified := range ht.notified {
		if isNotified && !ht.executed[handlerName] {
			if handler, exists := ht.handlers[handlerName]; exists {
				notifiedHandlers = append(notifiedHandlers, handler)
			}
		}
	}

	return notifiedHandlers
}

// GetAllHandlers returns all handlers registered with this tracker
func (ht *HandlerTracker) GetAllHandlers() []Task {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	var allHandlers []Task
	for _, handler := range ht.handlers {
		allHandlers = append(allHandlers, handler)
	}

	return allHandlers
}

// Reset clears all notification and execution status
func (ht *HandlerTracker) Reset() {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	ht.notified = make(map[string]bool)
	ht.executed = make(map[string]bool)

	common.LogDebug("Handler tracker reset", map[string]interface{}{
		"host": ht.hostName,
	})
}

// GetStats returns statistics about handler notifications and executions
func (ht *HandlerTracker) GetStats() map[string]interface{} {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	notifiedCount := 0
	executedCount := 0

	for _, isNotified := range ht.notified {
		if isNotified {
			notifiedCount++
		}
	}

	for _, isExecuted := range ht.executed {
		if isExecuted {
			executedCount++
		}
	}

	return map[string]interface{}{
		"total_handlers":    len(ht.handlers),
		"notified_handlers": notifiedCount,
		"executed_handlers": executedCount,
		"pending_handlers":  notifiedCount - executedCount,
	}
}
