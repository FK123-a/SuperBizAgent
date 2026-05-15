package tools

import (
	"SuperBizAgent/internal/ai/retriever"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func GetCurrentTimeJSON(_ context.Context) (string, error) {
	now := time.Now()
	timeOutput := GetCurrentTimeOutput{
		Success:      true,
		Seconds:      now.Unix(),
		Milliseconds: now.UnixMilli(),
		Microseconds: now.UnixMicro(),
		Timestamp:    now.Format("2006-01-02 15:04:05.000000"),
		Message:      "Current time retrieved successfully",
	}
	b, err := json.MarshalIndent(timeOutput, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func QueryPrometheusAlertsJSON(_ context.Context) (string, error) {
	result, err := queryPrometheusAlerts()
	if err != nil {
		alertsOut := PrometheusAlertsOutput{
			Success: false,
			Error:   err.Error(),
			Message: "Failed to query Prometheus alerts",
		}
		b, _ := json.MarshalIndent(alertsOut, "", "  ")
		return string(b), err
	}

	seenAlertNames := make(map[string]bool)
	simplifiedAlerts := make([]SimplifiedAlert, 0)
	for _, alert := range result.Data.Alerts {
		alertName := alert.Labels["alertname"]
		if seenAlertNames[alertName] {
			continue
		}
		seenAlertNames[alertName] = true
		simplifiedAlerts = append(simplifiedAlerts, SimplifiedAlert{
			AlertName:   alertName,
			Description: alert.Annotations["description"],
			State:       alert.State,
			ActiveAt:    alert.ActiveAt,
			Duration:    calculateDuration(alert.ActiveAt),
		})
	}

	alertsOut := PrometheusAlertsOutput{
		Success: true,
		Alerts:  simplifiedAlerts,
		Message: fmt.Sprintf("Successfully retrieved %d active alerts", len(simplifiedAlerts)),
	}
	b, err := json.MarshalIndent(alertsOut, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func QueryInternalDocsJSON(ctx context.Context, query string) (string, error) {
	rr, err := retriever.NewMilvusRetriever(ctx)
	if err != nil {
		return "", err
	}
	resp, err := rr.Retrieve(ctx, query)
	if err != nil {
		return "", err
	}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		return "", err
	}
	return string(respBytes), nil
}
