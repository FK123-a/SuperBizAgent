package plan_execute_replan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
	"time"
)

// FileStore persists executions/tasks/dependencies as JSON files under a root directory.
//
// Layout:
//
//	<root>/
//	  <execution_id>/
//	    execution.json
//	    deps.json
//	    task_<task_id>.json
//
// This is intentionally simple (single-process demo). For production, prefer a DB-backed store.
type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) (*FileStore, error) {
	if root == "" {
		return nil, errors.New("root path is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileStore{root: root}, nil
}

var safeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateID(id string) error {
	if !safeIDRe.MatchString(id) {
		return fmt.Errorf("invalid id: %q", id)
	}
	return nil
}

func (s *FileStore) execDir(executionID string) (string, error) {
	if err := validateID(executionID); err != nil {
		return "", err
	}
	return filepath.Join(s.root, executionID), nil
}

func (s *FileStore) execPath(executionID string) (string, error) {
	dir, err := s.execDir(executionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "execution.json"), nil
}

func (s *FileStore) depsPath(executionID string) (string, error) {
	dir, err := s.execDir(executionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "deps.json"), nil
}

func (s *FileStore) taskPath(executionID, taskID string) (string, error) {
	if err := validateID(executionID); err != nil {
		return "", err
	}
	if err := validateID(taskID); err != nil {
		return "", err
	}
	dir := filepath.Join(s.root, executionID)
	return filepath.Join(dir, "task_"+taskID+".json"), nil
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (s *FileStore) CreateExecution(_ context.Context, exec *Execution) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.execDir(exec.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	now := time.Now()
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = now
	}
	exec.UpdatedAt = now

	execFile, err := s.execPath(exec.ID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(execFile); err == nil {
		return errors.New("execution already exists")
	}

	// Initialize deps.json to an empty list so other operations are simpler.
	depsFile, err := s.depsPath(exec.ID)
	if err != nil {
		return err
	}
	if err := writeJSONFile(depsFile, []*Dependency{}); err != nil {
		return err
	}
	return writeJSONFile(execFile, exec)
}

func (s *FileStore) GetExecution(_ context.Context, executionID string) (*Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	execFile, err := s.execPath(executionID)
	if err != nil {
		return nil, err
	}
	var exec Execution
	if err := readJSONFile(execFile, &exec); err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *FileStore) UpdateExecution(_ context.Context, exec *Execution) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	execFile, err := s.execPath(exec.ID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(execFile); err != nil {
		return errors.New("execution not found")
	}
	exec.UpdatedAt = time.Now()
	return writeJSONFile(execFile, exec)
}

func (s *FileStore) CreateTasks(_ context.Context, tasks []*Task, deps []*Dependency) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(tasks) == 0 && len(deps) == 0 {
		return nil
	}

	var executionID string
	if len(tasks) > 0 {
		executionID = tasks[0].ExecutionID
	} else {
		executionID = deps[0].ExecutionID
	}
	dir, err := s.execDir(executionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	now := time.Now()
	for _, task := range tasks {
		if task.ExecutionID != executionID {
			return errors.New("mixed execution ids in tasks")
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
		}
		task.UpdatedAt = now
		taskFile, err := s.taskPath(executionID, task.ID)
		if err != nil {
			return err
		}
		if err := writeJSONFile(taskFile, task); err != nil {
			return err
		}
	}

	if len(deps) > 0 {
		for _, d := range deps {
			if d.ExecutionID != executionID {
				return errors.New("mixed execution ids in deps")
			}
		}
		depsFile, err := s.depsPath(executionID)
		if err != nil {
			return err
		}
		// Merge with existing deps to avoid accidental overwrite.
		var existing []*Dependency
		_ = readJSONFile(depsFile, &existing) // ignore if missing/invalid; will overwrite below
		existing = append(existing, deps...)
		return writeJSONFile(depsFile, existing)
	}

	return nil
}

func (s *FileStore) ListTasks(_ context.Context, executionID string) ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir, err := s.execDir(executionID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tasks []*Task
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if len(name) < len("task_.json") || name[:5] != "task_" || filepath.Ext(name) != ".json" {
			continue
		}
		full := filepath.Join(dir, name)
		var t Task
		if err := readJSONFile(full, &t); err != nil {
			return nil, err
		}
		tasks = append(tasks, &t)
	}

	slices.SortFunc(tasks, func(a, b *Task) int { return a.StepNo - b.StepNo })
	return tasks, nil
}

func (s *FileStore) GetTask(_ context.Context, executionID, taskID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskFile, err := s.taskPath(executionID, taskID)
	if err != nil {
		return nil, err
	}
	var t Task
	if err := readJSONFile(taskFile, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *FileStore) loadDepsLocked(executionID string) ([]*Dependency, error) {
	depsFile, err := s.depsPath(executionID)
	if err != nil {
		return nil, err
	}
	var deps []*Dependency
	if err := readJSONFile(depsFile, &deps); err != nil {
		// If deps.json is missing, treat as empty for demo purposes.
		if errors.Is(err, os.ErrNotExist) {
			return []*Dependency{}, nil
		}
		return nil, err
	}
	return deps, nil
}

func (s *FileStore) listTasksLocked(executionID string) ([]*Task, error) {
	dir, err := s.execDir(executionID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tasks []*Task
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if len(name) < len("task_.json") || name[:5] != "task_" || filepath.Ext(name) != ".json" {
			continue
		}
		full := filepath.Join(dir, name)
		var t Task
		if err := readJSONFile(full, &t); err != nil {
			return nil, err
		}
		tasks = append(tasks, &t)
	}

	slices.SortFunc(tasks, func(a, b *Task) int { return a.StepNo - b.StepNo })
	return tasks, nil
}

func (s *FileStore) ListRunnableTasks(ctx context.Context, executionID string) ([]*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, err := s.listTasksLocked(executionID)
	if err != nil {
		return nil, err
	}
	deps, err := s.loadDepsLocked(executionID)
	if err != nil {
		return nil, err
	}

	taskMap := map[string]*Task{}
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	blockedBy := map[string][]string{}
	for _, d := range deps {
		blockedBy[d.TaskID] = append(blockedBy[d.TaskID], d.BlockedByTaskID)
	}

	var runnable []*Task
	for _, t := range tasks {
		if t.Status != TaskPending {
			continue
		}
		ready := true
		for _, pre := range blockedBy[t.ID] {
			preTask, ok := taskMap[pre]
			if !ok || preTask.Status != TaskCompleted {
				ready = false
				break
			}
		}
		if ready {
			runnable = append(runnable, t)
		}
	}

	slices.SortFunc(runnable, func(a, b *Task) int { return a.StepNo - b.StepNo })
	return runnable, nil
}

func (s *FileStore) MarkTaskInProgress(_ context.Context, executionID, taskID, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskFile, err := s.taskPath(executionID, taskID)
	if err != nil {
		return false, err
	}
	var t Task
	if err := readJSONFile(taskFile, &t); err != nil {
		return false, err
	}
	if t.Status != TaskPending {
		return false, nil
	}
	t.Status = TaskInProgress
	t.Owner = owner
	t.UpdatedAt = time.Now()
	return true, writeJSONFile(taskFile, &t)
}

func (s *FileStore) MarkTaskCompleted(_ context.Context, executionID, taskID, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskFile, err := s.taskPath(executionID, taskID)
	if err != nil {
		return err
	}
	var t Task
	if err := readJSONFile(taskFile, &t); err != nil {
		return err
	}
	t.Status = TaskCompleted
	t.Result = result
	t.UpdatedAt = time.Now()
	return writeJSONFile(taskFile, &t)
}

func (s *FileStore) MarkTaskFailed(_ context.Context, executionID, taskID, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskFile, err := s.taskPath(executionID, taskID)
	if err != nil {
		return err
	}
	var t Task
	if err := readJSONFile(taskFile, &t); err != nil {
		return err
	}
	t.Status = TaskFailed
	t.ErrorMessage = errMsg
	t.UpdatedAt = time.Now()
	return writeJSONFile(taskFile, &t)
}

func (s *FileStore) ResetInProgressTasks(_ context.Context, executionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks, err := s.listTasksLocked(executionID)
	if err != nil {
		return 0, err
	}
	resetCount := 0
	for _, t := range tasks {
		if t.Status != TaskInProgress {
			continue
		}
		t.Status = TaskPending
		t.UpdatedAt = time.Now()
		taskFile, err := s.taskPath(executionID, t.ID)
		if err != nil {
			return resetCount, err
		}
		if err := writeJSONFile(taskFile, t); err != nil {
			return resetCount, err
		}
		resetCount++
	}
	return resetCount, nil
}
