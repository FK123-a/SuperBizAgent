package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Planner interface {
	Plan(ctx context.Context, query string) (*Plan, error)
}

type Executor interface {
	ExecuteTask(ctx context.Context, exec *Execution, task *Task) (string, error)
}

type Runner struct {
	store    TaskStore
	planner  Planner
	executor Executor
}

func NewPersistentRunner(store TaskStore, planner Planner, executor Executor) *Runner {
	return &Runner{
		store:    store,
		planner:  planner,
		executor: executor,
	}
}

func (r *Runner) Run(ctx context.Context, query string) (*Execution, []string, error) {
	exec := &Execution{
		ID:        uuid.NewString(),
		Query:     query,
		Status:    ExecutionRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := r.store.CreateExecution(ctx, exec); err != nil {
		return nil, nil, err
	}

	plan, err := r.planner.Plan(ctx, query)
	if err != nil {
		exec.Status = ExecutionFailed
		exec.FinalResult = err.Error()
		_ = r.store.UpdateExecution(ctx, exec)
		return nil, nil, err
	}

	tasks, deps := convertPlan(exec.ID, plan)
	if err := r.store.CreateTasks(ctx, tasks, deps); err != nil {
		return nil, nil, err
	}

	detail := []string{
		fmt.Sprintf("execution_id=%s", exec.ID),
		fmt.Sprintf("plan_tasks=%d", len(tasks)),
	}

	return r.continueExecution(ctx, exec, detail)
}

func (r *Runner) Resume(ctx context.Context, executionID string) (*Execution, []string, error) {
	exec, err := r.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, nil, err
	}
	resetCount, err := r.store.ResetInProgressTasks(ctx, executionID)
	if err != nil {
		return nil, nil, err
	}
	exec.Status = ExecutionRunning
	if err := r.store.UpdateExecution(ctx, exec); err != nil {
		return nil, nil, err
	}
	detail := []string{
		fmt.Sprintf("resume_execution_id=%s", exec.ID),
		fmt.Sprintf("reset_in_progress=%d", resetCount),
	}

	return r.continueExecution(ctx, exec, detail)
}

func (r *Runner) continueExecution(ctx context.Context, exec *Execution, detail []string) (*Execution, []string, error) {

	for {
		runnable, err := r.store.ListRunnableTasks(ctx, exec.ID)
		if err != nil {
			return nil, detail, err
		}
		if len(runnable) == 0 {
			break
		}

		for _, task := range runnable {
			ok, err := r.store.MarkTaskInProgress(ctx, exec.ID, task.ID, "persistent-runner")
			if err != nil {
				return nil, detail, err
			}
			if !ok {
				continue
			}

			detail = append(detail, fmt.Sprintf("start task[%s] %s", task.ID, task.Subject))
			result, execErr := r.executor.ExecuteTask(ctx, exec, task)
			if execErr != nil {
				_ = r.store.MarkTaskFailed(ctx, exec.ID, task.ID, execErr.Error())
				detail = append(detail, fmt.Sprintf("fail task[%s] %s: %v", task.ID, task.Subject, execErr))
				continue
			}

			if err := r.store.MarkTaskCompleted(ctx, exec.ID, task.ID, result); err != nil {
				return nil, detail, err
			}
			detail = append(detail, fmt.Sprintf("done task[%s] %s", task.ID, task.Subject))
		}
	}

	report, err := r.buildFinalReport(ctx, exec.ID)
	if err != nil {
		exec.Status = ExecutionFailed
		exec.FinalResult = err.Error()
		_ = r.store.UpdateExecution(ctx, exec)
		return nil, detail, err
	}

	exec.Status = ExecutionCompleted
	exec.FinalResult = report
	if err := r.store.UpdateExecution(ctx, exec); err != nil {
		return nil, detail, err
	}
	return exec, detail, nil
}

func convertPlan(executionID string, plan *Plan) ([]*Task, []*Dependency) {
	tasks := make([]*Task, 0, len(plan.Tasks))
	deps := make([]*Dependency, 0)

	for i, p := range plan.Tasks {
		tasks = append(tasks, &Task{
			ID:          p.ID,
			ExecutionID: executionID,
			Type:        p.Type,
			Subject:     p.Subject,
			Description: p.Description,
			Status:      TaskPending,
			StepNo:      i + 1,
		})
	}

	for _, d := range plan.Dependencies {
		for _, pre := range d.BlockedBy {
			deps = append(deps, &Dependency{
				ExecutionID:     executionID,
				TaskID:          d.TaskID,
				BlockedByTaskID: pre,
			})
		}
	}
	return tasks, deps
}

func (r *Runner) buildFinalReport(ctx context.Context, executionID string) (string, error) {
	tasks, err := r.store.ListTasks(ctx, executionID)
	if err != nil {
		return "", err
	}
	for _, task := range tasks {
		if task.Type == TaskTypeBuildFinalReport && task.Result != "" {
			return task.Result, nil
		}
	}

	type reportItem struct {
		Task   string `json:"task"`
		Type   string `json:"type"`
		Status string `json:"status"`
		Result string `json:"result,omitempty"`
		Error  string `json:"error,omitempty"`
	}

	items := make([]reportItem, 0, len(tasks))
	failed := 0
	for _, task := range tasks {
		items = append(items, reportItem{
			Task:   task.Subject,
			Type:   string(task.Type),
			Status: string(task.Status),
			Result: task.Result,
			Error:  task.ErrorMessage,
		})
		if task.Status == TaskFailed {
			failed++
		}
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}

	header := "AIOps tasks completed"
	if failed > 0 {
		header = fmt.Sprintf("AIOps tasks completed with %d failures", failed)
	}
	return strings.Join([]string{header, string(data)}, "\n"), nil
}

type StaticPlanner struct{}

func (p *StaticPlanner) Plan(_ context.Context, _ string) (*Plan, error) {
	return &Plan{
		Tasks: []*TaskPlan{
			{ID: "t1", Type: TaskTypeQueryActiveAlerts, Subject: "GetActiveAlerts", Description: "Query active Prometheus alerts"},
			{ID: "t2", Type: TaskTypeQueryInternalDocs, Subject: "QueryInternalDocs", Description: "Lookup internal docs for each alert"},
			{ID: "t3", Type: TaskTypeQueryLogs, Subject: "QueryRelatedLogs", Description: "Query logs related to alerts"},
			{ID: "t4", Type: TaskTypeBuildFinalReport, Subject: "BuildReport", Description: "Summarize findings into a report"},
		},
		Dependencies: []*DependencyPlan{
			{TaskID: "t2", BlockedBy: []string{"t1"}},
			{TaskID: "t3", BlockedBy: []string{"t1"}},
			{TaskID: "t4", BlockedBy: []string{"t2", "t3"}},
		},
	}, nil
}

type MockExecutor struct{}

func (e *MockExecutor) ExecuteTask(_ context.Context, exec *Execution, task *Task) (string, error) {
	return fmt.Sprintf("execution=%s task=%s subject=%s ok", exec.ID, task.ID, task.Subject), nil
}

func BuildPersistentPlanAgent(ctx context.Context, query string) (string, []string, error) {
	store, err := NewFileStore(filepath.Join(".", ".ai_ops_tasks"))
	if err != nil {
		return "", nil, err
	}
	planner := NewModelPlanner(filepath.Join(".", "manifest", "config", "config.yaml"))
	runner := NewPersistentRunner(
		store,
		planner,
		NewSafeExecutor(store),
	)
	exec, detail, err := runner.Run(ctx, query)
	if err != nil {
		return "", detail, err
	}
	return exec.FinalResult, detail, nil
}

func ResumePersistentPlanAgent(ctx context.Context, executionID string) (string, []string, error) {
	store, err := NewFileStore(filepath.Join(".", ".ai_ops_tasks"))
	if err != nil {
		return "", nil, err
	}
	planner := NewModelPlanner(filepath.Join(".", "manifest", "config", "config.yaml"))
	runner := NewPersistentRunner(
		store,
		planner,
		NewSafeExecutor(store),
	)
	exec, detail, err := runner.Resume(ctx, executionID)
	if err != nil {
		return "", detail, err
	}
	return exec.FinalResult, detail, nil
}
