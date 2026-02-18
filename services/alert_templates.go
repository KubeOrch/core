package services

import "github.com/KubeOrch/core/models"

// AlertTemplate represents a predefined alert rule template
type AlertTemplate struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Category    models.AlertRuleType     `json:"category"`
	Severity    models.AlertSeverity     `json:"severity"`
	Conditions  []models.AlertCondition  `json:"conditions"`
	EvaluationInterval int              `json:"evaluationInterval"` // seconds
	CooldownPeriod     int              `json:"cooldownPeriod"`     // seconds
}

var predefinedTemplates = []AlertTemplate{
	{
		ID:          "cluster-high-cpu",
		Name:        "High CPU Usage",
		Description: "Alert when cluster CPU usage exceeds 80% for 5 minutes",
		Category:    models.AlertRuleTypeCluster,
		Severity:    models.AlertSeverityWarning,
		Conditions: []models.AlertCondition{
			{Metric: "cpu_percentage", Operator: models.AlertConditionGT, Value: 80, Duration: 300},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "cluster-high-memory",
		Name:        "High Memory Usage",
		Description: "Alert when cluster memory usage exceeds 85% for 5 minutes",
		Category:    models.AlertRuleTypeCluster,
		Severity:    models.AlertSeverityWarning,
		Conditions: []models.AlertCondition{
			{Metric: "memory_percentage", Operator: models.AlertConditionGT, Value: 85, Duration: 300},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "cluster-disconnected",
		Name:        "Cluster Disconnected",
		Description: "Alert when a cluster becomes disconnected for 2 minutes",
		Category:    models.AlertRuleTypeCluster,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "cluster_status", Operator: models.AlertConditionEQ, Value: "disconnected", Duration: 120},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     600,
	},
	{
		ID:          "cluster-node-not-ready",
		Name:        "Node Not Ready",
		Description: "Alert when a cluster node is not in Ready state",
		Category:    models.AlertRuleTypeCluster,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "node_ready", Operator: models.AlertConditionEQ, Value: false, Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "workflow-execution-failed",
		Name:        "Workflow Execution Failed",
		Description: "Alert when a workflow run fails",
		Category:    models.AlertRuleTypeWorkflow,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "run_status", Operator: models.AlertConditionEQ, Value: "failed", Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     60,
	},
	{
		ID:          "workflow-execution-timeout",
		Name:        "Workflow Execution Timeout",
		Description: "Alert when a workflow run exceeds 30 minutes",
		Category:    models.AlertRuleTypeWorkflow,
		Severity:    models.AlertSeverityWarning,
		Conditions: []models.AlertCondition{
			{Metric: "run_duration", Operator: models.AlertConditionGT, Value: 1800, Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "workflow-consecutive-failures",
		Name:        "Consecutive Workflow Failures",
		Description: "Alert when a workflow fails more than 3 times consecutively",
		Category:    models.AlertRuleTypeWorkflow,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "consecutive_failures", Operator: models.AlertConditionGT, Value: 3, Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     600,
	},
	{
		ID:          "resource-pod-crashloop",
		Name:        "Pod CrashLoopBackOff",
		Description: "Alert when a pod enters CrashLoopBackOff state",
		Category:    models.AlertRuleTypeResource,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "container_state", Operator: models.AlertConditionEQ, Value: "CrashLoopBackOff", Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "resource-pod-oom",
		Name:        "Pod OOM Killed",
		Description: "Alert when a pod is terminated due to out of memory",
		Category:    models.AlertRuleTypeResource,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "termination_reason", Operator: models.AlertConditionEQ, Value: "OOMKilled", Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "resource-replica-mismatch",
		Name:        "Replica Count Mismatch",
		Description: "Alert when ready replicas are less than desired for 5 minutes",
		Category:    models.AlertRuleTypeResource,
		Severity:    models.AlertSeverityWarning,
		Conditions: []models.AlertCondition{
			{Metric: "ready_replicas", Operator: models.AlertConditionLT, Value: "desired_replicas", Duration: 300},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
	{
		ID:          "resource-pvc-full",
		Name:        "PVC Nearly Full",
		Description: "Alert when persistent volume claim usage exceeds 90%",
		Category:    models.AlertRuleTypeResource,
		Severity:    models.AlertSeverityCritical,
		Conditions: []models.AlertCondition{
			{Metric: "pvc_usage_percentage", Operator: models.AlertConditionGT, Value: 90, Duration: 0},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     600,
	},
	{
		ID:          "resource-pod-pending",
		Name:        "Pod Stuck Pending",
		Description: "Alert when a pod remains in Pending state for 10 minutes",
		Category:    models.AlertRuleTypeResource,
		Severity:    models.AlertSeverityWarning,
		Conditions: []models.AlertCondition{
			{Metric: "pod_phase", Operator: models.AlertConditionEQ, Value: "Pending", Duration: 600},
		},
		EvaluationInterval: 60,
		CooldownPeriod:     300,
	},
}

// GetAllTemplates returns all predefined alert templates
func GetAllTemplates() []AlertTemplate {
	return predefinedTemplates
}

// GetTemplateByID returns a specific template by its ID
func GetTemplateByID(id string) *AlertTemplate {
	for _, t := range predefinedTemplates {
		if t.ID == id {
			return &t
		}
	}
	return nil
}
