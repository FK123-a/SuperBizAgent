package plan_execute_replan

import "time"

type ExecutionStatus string

const (
	ExecutionPending   ExecutionStatus = "pending"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionCompleted ExecutionStatus = "completed"
	ExecutionFailed    ExecutionStatus = "failed"
)

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
)

type TaskType string

const (
	TaskTypeQueryActiveAlerts TaskType = "query_active_alerts"
	TaskTypeGetCurrentTime    TaskType = "get_current_time"
	TaskTypeQueryInternalDocs TaskType = "query_internal_docs"
	TaskTypeQueryLogs         TaskType = "query_logs"
	TaskTypeSummarizeAlerts   TaskType = "summarize_alerts"
	TaskTypeBuildFinalReport  TaskType = "build_final_report"
)

type Execution struct {
	ID          string
	Query       string
	Status      ExecutionStatus
	FinalResult string
	Round       int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Task struct {
	ID           string
	ExecutionID  string
	Type         TaskType
	Subject      string
	Description  string
	Status       TaskStatus
	StepNo       int
	Result       string
	ErrorMessage string
	Owner        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Dependency struct {
	ExecutionID     string
	TaskID          string
	BlockedByTaskID string
}

type Plan struct {
	Tasks        []*TaskPlan       `json:"tasks"`
	Dependencies []*DependencyPlan `json:"dependencies"`
}

type TaskPlan struct {
	ID          string   `json:"id"`
	Type        TaskType `json:"type"`
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
}

type DependencyPlan struct {
	TaskID    string   `json:"taskId"`
	BlockedBy []string `json:"blockedBy"`
}
