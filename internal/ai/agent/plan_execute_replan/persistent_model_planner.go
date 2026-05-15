package plan_execute_replan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

type plannerConfig struct {
	DSThinkChatModel struct {
		APIKey  string `yaml:"api_key"`
		BaseURL string `yaml:"base_url"`
		Model   string `yaml:"model"`
	} `yaml:"ds_think_chat_model"`
}

type ModelPlanner struct {
	configPath string
}

func NewModelPlanner(configPath string) *ModelPlanner {
	return &ModelPlanner{configPath: configPath}
}

func (p *ModelPlanner) Plan(ctx context.Context, query string) (*Plan, error) {
	cfg, err := loadPlannerConfig(p.configPath)
	if err != nil {
		return nil, err
	}

	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		Model:   cfg.DSThinkChatModel.Model,
		APIKey:  cfg.DSThinkChatModel.APIKey,
		BaseURL: cfg.DSThinkChatModel.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	chain := compose.NewChain[[]*schema.Message, *schema.Message]()
	chain.AppendChatModel(chatModel, compose.WithNodeName("planner_model"))
	runnable, err := chain.Compile(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := runnable.Invoke(ctx, []*schema.Message{
		schema.SystemMessage(modelPlannerSystemPrompt),
		schema.UserMessage(buildPlannerUserPrompt(query)),
	})
	if err != nil {
		return nil, err
	}

	content := extractJSON(resp.Content)
	var plan Plan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("parse planner json failed: %w; raw=%s", err, resp.Content)
	}
	if err := ValidatePlan(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func loadPlannerConfig(configPath string) (*plannerConfig, error) {
	if configPath == "" {
		return nil, errors.New("planner config path is empty")
	}
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var cfg plannerConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	if cfg.DSThinkChatModel.Model == "" || cfg.DSThinkChatModel.APIKey == "" || cfg.DSThinkChatModel.BaseURL == "" {
		return nil, fmt.Errorf("planner config missing required ds_think_chat_model fields: %s", filepath.Clean(configPath))
	}
	return &cfg, nil
}

func extractJSON(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return trimmed
	}

	if strings.Contains(trimmed, "```") {
		start := strings.Index(trimmed, "```")
		rest := trimmed[start+3:]
		if strings.HasPrefix(rest, "json") {
			rest = rest[4:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return strings.TrimSpace(trimmed[start : end+1])
	}
	return trimmed
}

const modelPlannerSystemPrompt = `You are a task planning model for an AIOps agent.

Your job is to decompose the user's goal into a small executable DAG of tasks and dependencies.

Output rules:
1. Return JSON only. No markdown, no explanation.
2. Use this shape exactly:
{
  "tasks": [
    {"id":"t1","type":"query_active_alerts","subject":"...","description":"..."}
  ],
  "dependencies": [
    {"taskId":"t2","blockedBy":["t1"]}
  ]
}
3. Task IDs must be unique and use the form t1, t2, t3...
4. Every task must use one of these types only:
   - query_active_alerts
   - get_current_time
   - query_internal_docs
   - query_logs
   - summarize_alerts
   - build_final_report
5. Keep tasks concrete and executable.
6. Prefer 3-8 tasks.
7. Only create dependency edges that are truly necessary.
8. If tasks can run in parallel, do not force a dependency.
9. Ensure the dependency graph is acyclic.
10. Include a final summary/report task when appropriate.
11. Do not invent external systems beyond the user request and provided constraints.`

func buildPlannerUserPrompt(query string) string {
	return strings.TrimSpace(`
Please decompose the following AIOps goal into tasks and dependencies.

Constraints:
- The agent may need to query active alerts first.
- If time-based parameters are needed, add a get_current_time task before dependent tasks.
- Internal docs lookup usually depends on alert information.
- Log queries usually depend on alert information.
- Final reporting usually depends on previous evidence collection.
- Respect the user's explicit ordering constraints if present.

User goal:
` + query)
}
