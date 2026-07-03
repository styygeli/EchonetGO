package api

import (
	"sync"
)

// Readiness tracks which components have finished initialization.
type Readiness struct {
	mu         sync.RWMutex
	registered map[string]struct{}
	ready      map[string]bool
}

func NewReadiness() *Readiness {
	return &Readiness{
		registered: make(map[string]struct{}),
		ready:      make(map[string]bool),
	}
}

// Register declares a component that must become ready. Call before starting its goroutine.
func (r *Readiness) Register(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registered[name] = struct{}{}
	r.ready[name] = false
}

func (r *Readiness) MarkReady(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.registered[name]; ok {
		r.ready[name] = true
	}
}

// Status returns whether all registered components are ready and a per-component map.
func (r *Readiness) Status() (allReady bool, components map[string]bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	components = make(map[string]bool, len(r.registered))
	allReady = true
	for name := range r.registered {
		ready := r.ready[name]
		components[name] = ready
		if !ready {
			allReady = false
		}
	}
	return allReady, components
}
