package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AlertSeverity represents the severity level of an alert
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertRuleType represents the category of alert rule
type AlertRuleType string

const (
	AlertRuleTypeCluster  AlertRuleType = "cluster"
	AlertRuleTypeWorkflow AlertRuleType = "workflow"
	AlertRuleTypeResource AlertRuleType = "resource"
)

// AlertConditionOperator represents comparison operators for conditions
type AlertConditionOperator string

const (
	AlertConditionGT  AlertConditionOperator = "gt"
	AlertConditionGTE AlertConditionOperator = "gte"
	AlertConditionLT  AlertConditionOperator = "lt"
	AlertConditionLTE AlertConditionOperator = "lte"
	AlertConditionEQ  AlertConditionOperator = "eq"
	AlertConditionNEQ AlertConditionOperator = "neq"
)

// AlertEventStatus represents the current status of a triggered alert event
type AlertEventStatus string

const (
	AlertEventFiring       AlertEventStatus = "firing"
	AlertEventResolved     AlertEventStatus = "resolved"
	AlertEventAcknowledged AlertEventStatus = "acknowledged"
)

// NotificationChannelType represents the type of notification channel
type NotificationChannelType string

const (
	NotifChannelSlack     NotificationChannelType = "slack"
	NotifChannelDiscord   NotificationChannelType = "discord"
	NotifChannelTelegram  NotificationChannelType = "telegram"
	NotifChannelPagerDuty NotificationChannelType = "pagerduty"
	NotifChannelTeams     NotificationChannelType = "teams"
	NotifChannelWebhook   NotificationChannelType = "webhook"
)

// AlertCondition defines a single condition within an alert rule
type AlertCondition struct {
	Metric   string                 `bson:"metric" json:"metric"`
	Operator AlertConditionOperator `bson:"operator" json:"operator"`
	Value    interface{}            `bson:"value" json:"value"`
	Duration int                    `bson:"duration" json:"duration"` // seconds
}

// AlertRule defines a monitoring alert rule
type AlertRule struct {
	ID                     primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	UserID                 primitive.ObjectID   `bson:"user_id" json:"userId"`
	Name                   string               `bson:"name" json:"name"`
	Description            string               `bson:"description" json:"description"`
	Type                   AlertRuleType        `bson:"type" json:"type"`
	Severity               AlertSeverity        `bson:"severity" json:"severity"`
	Enabled                bool                 `bson:"enabled" json:"enabled"`
	Conditions             []AlertCondition     `bson:"conditions" json:"conditions"`
	ClusterIDs             []string             `bson:"cluster_ids,omitempty" json:"clusterIds,omitempty"`
	WorkflowIDs            []string             `bson:"workflow_ids,omitempty" json:"workflowIds,omitempty"`
	ResourceTypes          []string             `bson:"resource_types,omitempty" json:"resourceTypes,omitempty"`
	Namespaces             []string             `bson:"namespaces,omitempty" json:"namespaces,omitempty"`
	TemplateID             string               `bson:"template_id,omitempty" json:"templateId,omitempty"`
	IsPredefined           bool                 `bson:"is_predefined" json:"isPredefined"`
	NotificationChannelIDs []primitive.ObjectID `bson:"notification_channel_ids,omitempty" json:"notificationChannelIds,omitempty"`
	EvaluationInterval     int                  `bson:"evaluation_interval" json:"evaluationInterval"` // seconds
	CooldownPeriod         int                  `bson:"cooldown_period" json:"cooldownPeriod"`         // seconds
	LastTriggeredAt        *time.Time           `bson:"last_triggered_at,omitempty" json:"lastTriggeredAt,omitempty"`
	TriggerCount           int                  `bson:"trigger_count" json:"triggerCount"`
	CreatedAt              time.Time            `bson:"created_at" json:"createdAt"`
	UpdatedAt              time.Time            `bson:"updated_at" json:"updatedAt"`
}

// AlertEvent represents a triggered alert instance
type AlertEvent struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	RuleID         primitive.ObjectID `bson:"rule_id" json:"ruleId"`
	UserID         primitive.ObjectID `bson:"user_id" json:"userId"`
	Status         AlertEventStatus   `bson:"status" json:"status"`
	Severity       AlertSeverity      `bson:"severity" json:"severity"`
	RuleName       string             `bson:"rule_name" json:"ruleName"`
	RuleType       AlertRuleType      `bson:"rule_type" json:"ruleType"`
	Message        string             `bson:"message" json:"message"`
	Details        map[string]interface{} `bson:"details,omitempty" json:"details,omitempty"`
	ClusterID      string             `bson:"cluster_id,omitempty" json:"clusterId,omitempty"`
	ClusterName    string             `bson:"cluster_name,omitempty" json:"clusterName,omitempty"`
	WorkflowID     string             `bson:"workflow_id,omitempty" json:"workflowId,omitempty"`
	WorkflowName   string             `bson:"workflow_name,omitempty" json:"workflowName,omitempty"`
	ResourceName   string             `bson:"resource_name,omitempty" json:"resourceName,omitempty"`
	ResourceType   string             `bson:"resource_type,omitempty" json:"resourceType,omitempty"`
	FiredAt        time.Time          `bson:"fired_at" json:"firedAt"`
	ResolvedAt     *time.Time         `bson:"resolved_at,omitempty" json:"resolvedAt,omitempty"`
	AcknowledgedAt *time.Time         `bson:"acknowledged_at,omitempty" json:"acknowledgedAt,omitempty"`
	AcknowledgedBy *primitive.ObjectID `bson:"acknowledged_by,omitempty" json:"acknowledgedBy,omitempty"`
}

// NotificationChannel represents a user notification endpoint
type NotificationChannel struct {
	ID        primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID     `bson:"user_id" json:"userId"`
	Name      string                 `bson:"name" json:"name"`
	Type      NotificationChannelType `bson:"type" json:"type"`
	Config    map[string]interface{} `bson:"config" json:"config"`
	Enabled   bool                   `bson:"enabled" json:"enabled"`
	CreatedAt time.Time              `bson:"created_at" json:"createdAt"`
	UpdatedAt time.Time              `bson:"updated_at" json:"updatedAt"`
}
