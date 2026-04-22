package task

import (
	"fmt"
	"sync"
)

// Store is a thread-safe in-memory task store.
type Store struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewStore creates a new task store.
func NewStore() *Store {
	return &Store{
		tasks: make(map[string]*Task),
	}
}

// Put stores a task.
func (s *Store) Put(t *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
}

// Get retrieves a task by ID.
func (s *Store) Get(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return t, nil
}

// List returns all tasks.
func (s *Store) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		result = append(result, t)
	}
	return result
}
