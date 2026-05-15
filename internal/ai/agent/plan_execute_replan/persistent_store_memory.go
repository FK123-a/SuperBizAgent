package plan_execute_replan

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// TaskStore is a minimal persistent-task interface for AIOps execution.
// The first version is in-memory so we can run through the flow end-to-end.
// Later this can be backed by MySQL/Redis/etc without changing Runner logic.
type TaskStore interface {
	CreateExecution(ctx context.Context, exec *Execution) error
	GetExecution(ctx context.Context, executionID string) (*Execution, error)
	UpdateExecution(ctx context.Context, exec *Execution) error

	CreateTasks(ctx context.Context, tasks []*Task, deps []*Dependency) error
	ListTasks(ctx context.Context, executionID string) ([]*Task, error)
	GetTask(ctx context.Context, executionID, taskID string) (*Task, error)

	ListRunnableTasks(ctx context.Context, executionID string) ([]*Task, error)
	MarkTaskInProgress(ctx context.Context, executionID, taskID, owner string) (bool, error)
	MarkTaskCompleted(ctx context.Context, executionID, taskID, result string) error
	MarkTaskFailed(ctx context.Context, executionID, taskID, errMsg string) error
	ResetInProgressTasks(ctx context.Context, executionID string) (int, error)
}

type MemoryStore struct {
	mu           sync.Mutex
	executions   map[string]*Execution
	tasks        map[string]map[string]*Task
	dependencies map[string][]*Dependency
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		executions:   map[string]*Execution{},
		tasks:        map[string]map[string]*Task{},
		dependencies: map[string][]*Dependency{},
	}
}

func (m *MemoryStore) CreateExecution(_ context.Context, exec *Execution) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.executions[exec.ID]; ok {
		return errors.New("execution already exists")
	}
	now := time.Now()
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = now
	}
	exec.UpdatedAt = now
	m.executions[exec.ID] = exec
	m.tasks[exec.ID] = map[string]*Task{}
	return nil
}

func (m *MemoryStore) GetExecution(_ context.Context, executionID string) (*Execution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	exec, ok := m.executions[executionID]
	if !ok {
		return nil, errors.New("execution not found")
	}
	cp := *exec
	return &cp, nil
}

func (m *MemoryStore) UpdateExecution(_ context.Context, exec *Execution) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.executions[exec.ID]; !ok {
		return errors.New("execution not found")
	}
	exec.UpdatedAt = time.Now()
	m.executions[exec.ID] = exec
	return nil
}

func (m *MemoryStore) CreateTasks(_ context.Context, tasks []*Task, deps []*Dependency) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, task := range tasks {
		if _, ok := m.tasks[task.ExecutionID]; !ok {
			m.tasks[task.ExecutionID] = map[string]*Task{}
		}
		now := time.Now()
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
		}
		task.UpdatedAt = now
		m.tasks[task.ExecutionID][task.ID] = task
	}
	for _, dep := range deps {
		m.dependencies[dep.ExecutionID] = append(m.dependencies[dep.ExecutionID], dep)
	}
	return nil
}

func (m *MemoryStore) ListTasks(_ context.Context, executionID string) ([]*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return nil, errors.New("execution tasks not found")
	}
	out := make([]*Task, 0, len(taskMap))
	for _, t := range taskMap {
		cp := *t
		out = append(out, &cp)
	}
	slices.SortFunc(out, func(a, b *Task) int { return a.StepNo - b.StepNo })
	return out, nil
}

func (m *MemoryStore) GetTask(_ context.Context, executionID, taskID string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return nil, errors.New("execution tasks not found")
	}
	task, ok := taskMap[taskID]
	if !ok {
		return nil, errors.New("task not found")
	}
	cp := *task
	return &cp, nil
}

func (m *MemoryStore) ListRunnableTasks(_ context.Context, executionID string) ([]*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return nil, errors.New("execution tasks not found")
	}

	// Build adjacency: task -> prerequisites.
	blockedBy := map[string][]string{}
	for _, dep := range m.dependencies[executionID] {
		blockedBy[dep.TaskID] = append(blockedBy[dep.TaskID], dep.BlockedByTaskID)
	}

	var runnable []*Task
	for _, task := range taskMap {
		if task.Status != TaskPending {
			continue
		}
		ready := true
		for _, pre := range blockedBy[task.ID] {
			preTask, ok := taskMap[pre]
			if !ok || preTask.Status != TaskCompleted {
				ready = false
				break
			}
		}
		if ready {
			cp := *task
			runnable = append(runnable, &cp)
		}
	}

	slices.SortFunc(runnable, func(a, b *Task) int { return a.StepNo - b.StepNo })
	return runnable, nil
}

func (m *MemoryStore) MarkTaskInProgress(_ context.Context, executionID, taskID, owner string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return false, errors.New("execution tasks not found")
	}
	task, ok := taskMap[taskID]
	if !ok {
		return false, errors.New("task not found")
	}
	if task.Status != TaskPending {
		return false, nil
	}
	task.Status = TaskInProgress
	task.Owner = owner
	task.UpdatedAt = time.Now()
	return true, nil
}

func (m *MemoryStore) MarkTaskCompleted(_ context.Context, executionID, taskID, result string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return errors.New("execution tasks not found")
	}
	task, ok := taskMap[taskID]
	if !ok {
		return errors.New("task not found")
	}
	task.Status = TaskCompleted
	task.Result = result
	task.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryStore) MarkTaskFailed(_ context.Context, executionID, taskID, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return errors.New("execution tasks not found")
	}
	task, ok := taskMap[taskID]
	if !ok {
		return errors.New("task not found")
	}
	task.Status = TaskFailed
	task.ErrorMessage = errMsg
	task.UpdatedAt = time.Now()
	return nil
}

func (m *MemoryStore) ResetInProgressTasks(_ context.Context, executionID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskMap, ok := m.tasks[executionID]
	if !ok {
		return 0, errors.New("execution tasks not found")
	}
	resetCount := 0
	for _, task := range taskMap {
		if task.Status == TaskInProgress {
			task.Status = TaskPending
			task.UpdatedAt = time.Now()
			resetCount++
		}
	}
	return resetCount, nil
}
