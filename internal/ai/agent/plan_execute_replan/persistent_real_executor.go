package plan_execute_replan

import (
	"SuperBizAgent/internal/ai/tools"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SafeExecutor struct {
	store TaskStore
}

func NewSafeExecutor(store TaskStore) *SafeExecutor {
	return &SafeExecutor{store: store}
}

func (e *SafeExecutor) ExecuteTask(ctx context.Context, exec *Execution, task *Task) (string, error) {
	switch task.Type {
	case TaskTypeQueryActiveAlerts:
		return tools.QueryPrometheusAlertsJSON(ctx)
	case TaskTypeGetCurrentTime:
		return tools.GetCurrentTimeJSON(ctx)
	case TaskTypeQueryInternalDocs:
		return e.executeQueryInternalDocs(ctx, exec, task)
	case TaskTypeQueryLogs:
		return e.executeQueryLogs(ctx, exec, task)
	case TaskTypeSummarizeAlerts:
		return e.executeSummarizeAlerts(ctx, exec, task)
	case TaskTypeBuildFinalReport:
		return e.executeBuildFinalReport(ctx, exec, task)
	default:
		return "", fmt.Errorf("unsupported task type: %s", task.Type)
	}
}

func (e *SafeExecutor) executeQueryInternalDocs(ctx context.Context, exec *Execution, task *Task) (string, error) {
	alertNames := e.findAlertNames(ctx, exec.ID)
	if len(alertNames) == 0 {
		alertNames = []string{exec.Query}
	}

	type docItem struct {
		Query    string `json:"query"`
		Success  bool   `json:"success"`
		Degraded bool   `json:"degraded,omitempty"`
		Result   string `json:"result,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	items := make([]docItem, 0, len(alertNames))
	for _, name := range alertNames {
		result, err := tools.QueryInternalDocsJSON(ctx, name)
		if err != nil {
			items = append(items, docItem{
				Query:    name,
				Success:  false,
				Degraded: true,
				Error:    err.Error(),
			})
			continue
		}
		items = append(items, docItem{
			Query:   name,
			Success: true,
			Result:  trimForStorage(result, 3000),
		})
	}
	return toJSON(map[string]any{
		"task_type": task.Type,
		"items":     items,
	}), nil
}

func (e *SafeExecutor) executeQueryLogs(ctx context.Context, exec *Execution, task *Task) (string, error) {
	alertNames := e.findAlertNames(ctx, exec.ID)
	return toJSON(map[string]any{
		"task_type": task.Type,
		"executed":  false,
		"degraded":  true,
		"reason":    "runtime log query is not wired for safe local execution yet",
		"alerts":    alertNames,
		"hint":      "replace this branch with MCP log invocation after validating runtime availability",
	}), nil
}

func (e *SafeExecutor) executeSummarizeAlerts(ctx context.Context, exec *Execution, task *Task) (string, error) {
	tasks, err := e.store.ListTasks(ctx, exec.ID)
	if err != nil {
		return "", err
	}
	var lines []string
	lines = append(lines, "Alert analysis summary")
	for _, t := range tasks {
		if t.ID == task.ID {
			continue
		}
		if t.Type == TaskTypeBuildFinalReport {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", t.Status, t.Subject))
		if t.Result != "" {
			lines = append(lines, "  "+trimForStorage(flattenWhitespace(t.Result), 240))
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (e *SafeExecutor) executeBuildFinalReport(ctx context.Context, exec *Execution, task *Task) (string, error) {
	tasks, err := e.store.ListTasks(ctx, exec.ID)
	if err != nil {
		return "", err
	}
	var lines []string
	lines = append(lines, "AIOps Analysis Report")
	lines = append(lines, "---")
	lines = append(lines, fmt.Sprintf("Execution ID: %s", exec.ID))
	lines = append(lines, "")
	for _, t := range tasks {
		if t.ID == task.ID {
			continue
		}
		lines = append(lines, fmt.Sprintf("## %s (%s)", t.Subject, t.Type))
		lines = append(lines, fmt.Sprintf("Status: %s", t.Status))
		if t.Result != "" {
			lines = append(lines, trimForStorage(t.Result, 1200))
		}
		if t.ErrorMessage != "" {
			lines = append(lines, "Error: "+t.ErrorMessage)
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n"), nil
}

func (e *SafeExecutor) findAlertNames(ctx context.Context, executionID string) []string {
	tasks, err := e.store.ListTasks(ctx, executionID)
	if err != nil {
		return nil
	}
	for _, t := range tasks {
		if t.Type != TaskTypeQueryActiveAlerts || t.Result == "" {
			continue
		}
		var payload struct {
			Alerts []struct {
				AlertName string `json:"alert_name"`
			} `json:"alerts"`
		}
		if err := json.Unmarshal([]byte(t.Result), &payload); err != nil {
			continue
		}
		var names []string
		for _, alert := range payload.Alerts {
			if alert.AlertName != "" {
				names = append(names, alert.AlertName)
			}
		}
		if len(names) > 0 {
			return names
		}
	}
	return nil
}

func toJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"success":false,"error":%q}`, err.Error())
	}
	return string(b)
}

func trimForStorage(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

func flattenWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}
