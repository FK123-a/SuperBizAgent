package plan_execute_replan

import (
	"errors"
	"fmt"
)

var allowedTaskTypes = map[TaskType]struct{}{
	TaskTypeQueryActiveAlerts: {},
	TaskTypeGetCurrentTime:    {},
	TaskTypeQueryInternalDocs: {},
	TaskTypeQueryLogs:         {},
	TaskTypeSummarizeAlerts:   {},
	TaskTypeBuildFinalReport:  {},
}

func ValidatePlan(plan *Plan) error {
	if plan == nil {
		return errors.New("plan is nil")
	}
	if len(plan.Tasks) == 0 {
		return errors.New("plan has no tasks")
	}

	taskMap := make(map[string]*TaskPlan, len(plan.Tasks))
	inDegree := make(map[string]int, len(plan.Tasks))
	graph := make(map[string][]string, len(plan.Tasks))

	for _, task := range plan.Tasks {
		if task == nil {
			return errors.New("plan contains nil task")
		}
		if task.ID == "" {
			return errors.New("task id is empty")
		}
		if task.Subject == "" {
			return fmt.Errorf("task %s subject is empty", task.ID)
		}
		if _, ok := allowedTaskTypes[task.Type]; !ok {
			return fmt.Errorf("task %s has invalid type: %s", task.ID, task.Type)
		}
		if _, exists := taskMap[task.ID]; exists {
			return fmt.Errorf("duplicate task id: %s", task.ID)
		}
		taskMap[task.ID] = task
		inDegree[task.ID] = 0
	}

	for _, dep := range plan.Dependencies {
		if dep == nil {
			return errors.New("plan contains nil dependency")
		}
		if dep.TaskID == "" {
			return errors.New("dependency taskId is empty")
		}
		if _, ok := taskMap[dep.TaskID]; !ok {
			return fmt.Errorf("dependency target task not found: %s", dep.TaskID)
		}
		for _, blockedBy := range dep.BlockedBy {
			if blockedBy == "" {
				return fmt.Errorf("dependency blockedBy is empty for task %s", dep.TaskID)
			}
			if blockedBy == dep.TaskID {
				return fmt.Errorf("task %s cannot depend on itself", dep.TaskID)
			}
			if _, ok := taskMap[blockedBy]; !ok {
				return fmt.Errorf("dependency prerequisite task not found: %s", blockedBy)
			}
			graph[blockedBy] = append(graph[blockedBy], dep.TaskID)
			inDegree[dep.TaskID]++
		}
	}

	var queue []string
	for taskID, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, taskID)
		}
	}
	if len(queue) == 0 {
		return errors.New("plan has no entry task; possible cycle")
	}

	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range graph[cur] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(plan.Tasks) {
		return errors.New("plan has cyclic dependencies")
	}
	return nil
}
