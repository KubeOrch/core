package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type AlertService struct {
	repo   *repositories.AlertRepository
	logger *logrus.Logger
}

func NewAlertService() *AlertService {
	return &AlertService{
		repo:   repositories.NewAlertRepository(),
		logger: logrus.New(),
	}
}

// Singleton
var (
	alertServiceInstance *AlertService
	alertServiceOnce    sync.Once
)

func GetAlertService() *AlertService {
	alertServiceOnce.Do(func() {
		alertServiceInstance = NewAlertService()
	})
	return alertServiceInstance
}

// --- Rules ---

func (s *AlertService) CreateRule(ctx context.Context, rule *models.AlertRule) error {
	if rule.EvaluationInterval == 0 {
		rule.EvaluationInterval = 60
	}
	if rule.CooldownPeriod == 0 {
		rule.CooldownPeriod = 300
	}
	return s.repo.CreateRule(ctx, rule)
}

func (s *AlertService) GetRule(ctx context.Context, id primitive.ObjectID) (*models.AlertRule, error) {
	return s.repo.GetRuleByID(ctx, id)
}

func (s *AlertService) ListRules(ctx context.Context, userID primitive.ObjectID, ruleType string, severity string, enabled *bool) ([]models.AlertRule, error) {
	return s.repo.ListRulesByUser(ctx, userID, ruleType, severity, enabled)
}

func (s *AlertService) UpdateRule(ctx context.Context, rule *models.AlertRule) error {
	return s.repo.UpdateRule(ctx, rule)
}

func (s *AlertService) DeleteRule(ctx context.Context, id primitive.ObjectID) error {
	return s.repo.DeleteRule(ctx, id)
}

func (s *AlertService) ToggleRule(ctx context.Context, id primitive.ObjectID, enabled bool) error {
	return s.repo.ToggleRule(ctx, id, enabled)
}

// EnableTemplate creates an AlertRule from a predefined template
func (s *AlertService) EnableTemplate(ctx context.Context, userID primitive.ObjectID, templateID string, clusterIDs []string, workflowIDs []string, namespaces []string) (*models.AlertRule, error) {
	tmpl := GetTemplateByID(templateID)
	if tmpl == nil {
		return nil, fmt.Errorf("template not found: %s", templateID)
	}

	rule := &models.AlertRule{
		UserID:             userID,
		Name:               tmpl.Name,
		Description:        tmpl.Description,
		Type:               tmpl.Category,
		Severity:           tmpl.Severity,
		Enabled:            true,
		Conditions:         tmpl.Conditions,
		ClusterIDs:         clusterIDs,
		WorkflowIDs:        workflowIDs,
		Namespaces:         namespaces,
		TemplateID:         tmpl.ID,
		IsPredefined:       true,
		EvaluationInterval: tmpl.EvaluationInterval,
		CooldownPeriod:     tmpl.CooldownPeriod,
	}

	if err := s.repo.CreateRule(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

// GetOverviewStats returns alert overview statistics
func (s *AlertService) GetOverviewStats(ctx context.Context, userID primitive.ObjectID) (map[string]interface{}, error) {
	activeCount, err := s.repo.CountActiveEventsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	allRules, err := s.repo.ListRulesByUser(ctx, userID, "", "", nil)
	if err != nil {
		return nil, err
	}

	enabledCount := 0
	for _, r := range allRules {
		if r.Enabled {
			enabledCount++
		}
	}

	severityBreakdown, err := s.repo.CountEventsBySeverity(ctx, userID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"activeAlerts":      activeCount,
		"totalRules":        len(allRules),
		"enabledRules":      enabledCount,
		"severityBreakdown": severityBreakdown,
	}, nil
}

// --- Events ---

func (s *AlertService) ListEvents(ctx context.Context, userID primitive.ObjectID, severity string, status string, ruleType string, page int, limit int) ([]models.AlertEvent, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.ListEventsByUser(ctx, userID, severity, status, ruleType, page, limit)
}

func (s *AlertService) AcknowledgeEvent(ctx context.Context, eventID primitive.ObjectID, userID primitive.ObjectID) error {
	return s.repo.UpdateEventStatus(ctx, eventID, models.AlertEventAcknowledged, &userID)
}

func (s *AlertService) ResolveEvent(ctx context.Context, eventID primitive.ObjectID, userID primitive.ObjectID) error {
	return s.repo.UpdateEventStatus(ctx, eventID, models.AlertEventResolved, &userID)
}

// FireAlert creates an alert event, publishes via SSE, and dispatches webhooks
func (s *AlertService) FireAlert(ctx context.Context, rule *models.AlertRule, message string, details map[string]interface{}) error {
	// Check cooldown
	if rule.LastTriggeredAt != nil {
		cooldown := time.Duration(rule.CooldownPeriod) * time.Second
		if time.Since(*rule.LastTriggeredAt) < cooldown {
			return nil // still in cooldown
		}
	}

	event := &models.AlertEvent{
		RuleID:   rule.ID,
		UserID:   rule.UserID,
		Status:   models.AlertEventFiring,
		Severity: rule.Severity,
		RuleName: rule.Name,
		RuleType: rule.Type,
		Message:  message,
		Details:  details,
		FiredAt:  time.Now(),
	}

	// Set context from details
	if v, ok := details["cluster_id"]; ok {
		event.ClusterID = fmt.Sprintf("%v", v)
	}
	if v, ok := details["cluster_name"]; ok {
		event.ClusterName = fmt.Sprintf("%v", v)
	}
	if v, ok := details["workflow_id"]; ok {
		event.WorkflowID = fmt.Sprintf("%v", v)
	}
	if v, ok := details["workflow_name"]; ok {
		event.WorkflowName = fmt.Sprintf("%v", v)
	}
	if v, ok := details["resource_name"]; ok {
		event.ResourceName = fmt.Sprintf("%v", v)
	}
	if v, ok := details["resource_type"]; ok {
		event.ResourceType = fmt.Sprintf("%v", v)
	}

	if err := s.repo.CreateEvent(ctx, event); err != nil {
		return err
	}

	// Update rule trigger time
	if err := s.repo.UpdateRuleTrigger(ctx, rule.ID); err != nil {
		s.logger.WithError(err).Warn("Failed to update rule trigger time")
	}

	// Publish SSE event
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "alerts",
		StreamKey: fmt.Sprintf("alerts:%s", rule.UserID.Hex()),
		EventType: "alert_fired",
		Data: map[string]interface{}{
			"event_id": event.ID.Hex(),
			"rule_id":  rule.ID.Hex(),
			"severity": string(event.Severity),
			"message":  event.Message,
			"type":     string(event.RuleType),
		},
	})

	// Dispatch webhooks in background
	if len(rule.NotificationChannelIDs) > 0 {
		go s.dispatchWebhooks(rule, event)
	}

	return nil
}

// ResolveAlertsByRule resolves all firing events for a rule and publishes resolution
func (s *AlertService) ResolveAlertsByRule(ctx context.Context, rule *models.AlertRule) error {
	activeEvents, err := s.repo.GetActiveEventsByRule(ctx, rule.ID)
	if err != nil {
		return err
	}
	if len(activeEvents) == 0 {
		return nil
	}

	if err := s.repo.ResolveEventsByRule(ctx, rule.ID); err != nil {
		return err
	}

	// Publish resolution event via SSE
	broadcaster := GetSSEBroadcaster()
	broadcaster.Publish(StreamEvent{
		Type:      "alerts",
		StreamKey: fmt.Sprintf("alerts:%s", rule.UserID.Hex()),
		EventType: "alert_resolved",
		Data: map[string]interface{}{
			"rule_id":  rule.ID.Hex(),
			"severity": string(rule.Severity),
			"message":  fmt.Sprintf("Alert '%s' has been resolved", rule.Name),
		},
	})

	return nil
}

// --- Notification Channels ---

func (s *AlertService) CreateChannel(ctx context.Context, channel *models.NotificationChannel) error {
	return s.repo.CreateChannel(ctx, channel)
}

func (s *AlertService) GetChannel(ctx context.Context, id primitive.ObjectID) (*models.NotificationChannel, error) {
	return s.repo.GetChannelByID(ctx, id)
}

func (s *AlertService) ListChannels(ctx context.Context, userID primitive.ObjectID) ([]models.NotificationChannel, error) {
	return s.repo.ListChannelsByUser(ctx, userID)
}

func (s *AlertService) UpdateChannel(ctx context.Context, channel *models.NotificationChannel) error {
	return s.repo.UpdateChannel(ctx, channel)
}

func (s *AlertService) DeleteChannel(ctx context.Context, id primitive.ObjectID) error {
	return s.repo.DeleteChannel(ctx, id)
}

// TestChannel sends a test notification to the channel using type-specific formatting
func (s *AlertService) TestChannel(ctx context.Context, channel *models.NotificationChannel) error {
	testEvent := &models.AlertEvent{
		RuleName: "Test Alert",
		RuleType: models.AlertRuleTypeCluster,
		Severity: models.AlertSeverityInfo,
		Message:  "This is a test notification from KubeOrch",
		Details: map[string]interface{}{
			"cluster_name": "test-cluster",
			"test":         true,
		},
		FiredAt: time.Now(),
	}
	return s.dispatchToChannel(channel, testEvent)
}

// dispatchWebhooks sends alert event to all configured notification channels
func (s *AlertService) dispatchWebhooks(rule *models.AlertRule, event *models.AlertEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	channels, err := s.repo.GetChannelsByIDs(ctx, rule.NotificationChannelIDs)
	if err != nil {
		s.logger.WithError(err).Error("Failed to get notification channels")
		return
	}

	for _, ch := range channels {
		go func(channel models.NotificationChannel) {
			if err := s.dispatchToChannel(&channel, event); err != nil {
				s.logger.WithFields(logrus.Fields{
					"channel": channel.Name,
					"type":    channel.Type,
					"error":   err.Error(),
				}).Error("Failed to send notification")
			}
		}(ch)
	}
}

// sendWebhook sends an HTTP POST to a webhook URL
func (s *AlertService) sendWebhook(channel *models.NotificationChannel, payload map[string]interface{}) error {
	url, ok := channel.Config["url"].(string)
	if !ok || url == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add custom headers from config
	if headers, ok := channel.Config["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// dispatchToChannel routes the alert event to the appropriate sender based on channel type
func (s *AlertService) dispatchToChannel(channel *models.NotificationChannel, event *models.AlertEvent) error {
	switch channel.Type {
	case models.NotifChannelSlack:
		return s.sendSlack(channel, event)
	case models.NotifChannelDiscord:
		return s.sendDiscord(channel, event)
	case models.NotifChannelTelegram:
		return s.sendTelegram(channel, event)
	case models.NotifChannelPagerDuty:
		return s.sendPagerDuty(channel, event)
	case models.NotifChannelTeams:
		return s.sendTeams(channel, event)
	case models.NotifChannelWebhook:
		payload := map[string]interface{}{
			"type":     "alert",
			"severity": string(event.Severity),
			"ruleName": event.RuleName,
			"ruleType": string(event.RuleType),
			"message":  event.Message,
			"details":  event.Details,
			"firedAt":  event.FiredAt.Format(time.RFC3339),
		}
		if !event.ID.IsZero() {
			payload["eventId"] = event.ID.Hex()
		}
		return s.sendWebhook(channel, payload)
	default:
		return fmt.Errorf("unsupported channel type: %s", channel.Type)
	}
}

// severityColor returns a hex color int for the severity level
func severityColor(severity models.AlertSeverity) int {
	switch severity {
	case models.AlertSeverityCritical:
		return 0xED4245
	case models.AlertSeverityWarning:
		return 0xFEE75C
	default:
		return 0x5865F2
	}
}

// severityColorHex returns a hex color string for Slack
func severityColorHex(severity models.AlertSeverity) string {
	switch severity {
	case models.AlertSeverityCritical:
		return "#ED4245"
	case models.AlertSeverityWarning:
		return "#FEE75C"
	default:
		return "#5865F2"
	}
}

// sendSlack sends a Block Kit formatted message to a Slack incoming webhook
func (s *AlertService) sendSlack(channel *models.NotificationChannel, event *models.AlertEvent) error {
	webhookURL, ok := channel.Config["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return fmt.Errorf("slack webhook_url not configured")
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": severityColorHex(event.Severity),
				"blocks": []map[string]interface{}{
					{
						"type": "section",
						"text": map[string]interface{}{
							"type": "mrkdwn",
							"text": fmt.Sprintf("*%s*\n%s", event.RuleName, event.Message),
						},
					},
					{
						"type": "section",
						"fields": []map[string]interface{}{
							{"type": "mrkdwn", "text": fmt.Sprintf("*Severity:*\n%s", event.Severity)},
							{"type": "mrkdwn", "text": fmt.Sprintf("*Type:*\n%s", event.RuleType)},
							{"type": "mrkdwn", "text": fmt.Sprintf("*Cluster:*\n%s", event.ClusterName)},
							{"type": "mrkdwn", "text": fmt.Sprintf("*Fired At:*\n%s", event.FiredAt.Format(time.RFC3339))},
						},
					},
				},
			},
		},
	}

	return s.postJSON(webhookURL, payload, nil)
}

// sendDiscord sends an embed message to a Discord webhook
func (s *AlertService) sendDiscord(channel *models.NotificationChannel, event *models.AlertEvent) error {
	webhookURL, ok := channel.Config["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return fmt.Errorf("discord webhook_url not configured")
	}

	fields := []map[string]interface{}{
		{"name": "Severity", "value": string(event.Severity), "inline": true},
		{"name": "Type", "value": string(event.RuleType), "inline": true},
	}
	if event.ClusterName != "" {
		fields = append(fields, map[string]interface{}{"name": "Cluster", "value": event.ClusterName, "inline": true})
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       event.RuleName,
				"description": event.Message,
				"color":       severityColor(event.Severity),
				"fields":      fields,
				"timestamp":   event.FiredAt.Format(time.RFC3339),
				"footer":      map[string]interface{}{"text": "KubeOrch Alert"},
			},
		},
	}

	return s.postJSON(webhookURL, payload, nil)
}

// sendTelegram sends an HTML-formatted message via the Telegram Bot API
func (s *AlertService) sendTelegram(channel *models.NotificationChannel, event *models.AlertEvent) error {
	botToken, ok := channel.Config["bot_token"].(string)
	if !ok || botToken == "" {
		return fmt.Errorf("telegram bot_token not configured")
	}
	chatID, ok := channel.Config["chat_id"].(string)
	if !ok || chatID == "" {
		return fmt.Errorf("telegram chat_id not configured")
	}

	text := fmt.Sprintf(
		"<b>%s</b> [%s]\n\n%s\n\n<b>Type:</b> %s\n<b>Cluster:</b> %s\n<b>Fired:</b> %s",
		event.RuleName,
		event.Severity,
		event.Message,
		event.RuleType,
		event.ClusterName,
		event.FiredAt.Format(time.RFC3339),
	)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	return s.postJSON(url, payload, nil)
}

// sendPagerDuty sends an event to PagerDuty Events API v2
func (s *AlertService) sendPagerDuty(channel *models.NotificationChannel, event *models.AlertEvent) error {
	routingKey, ok := channel.Config["routing_key"].(string)
	if !ok || routingKey == "" {
		return fmt.Errorf("pagerduty routing_key not configured")
	}

	pdSeverity := "warning"
	switch event.Severity {
	case models.AlertSeverityCritical:
		pdSeverity = "critical"
	case models.AlertSeverityWarning:
		pdSeverity = "warning"
	case models.AlertSeverityInfo:
		pdSeverity = "info"
	}

	dedupKey := fmt.Sprintf("kubeorch-%s-%s", event.RuleName, string(event.RuleType))
	if !event.RuleID.IsZero() {
		dedupKey = fmt.Sprintf("kubeorch-%s", event.RuleID.Hex())
	}

	payload := map[string]interface{}{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"dedup_key":    dedupKey,
		"payload": map[string]interface{}{
			"summary":  fmt.Sprintf("[KubeOrch] %s: %s", event.RuleName, event.Message),
			"severity": pdSeverity,
			"source":   "kubeorch",
			"group":    string(event.RuleType),
			"custom_details": map[string]interface{}{
				"rule_name":    event.RuleName,
				"rule_type":    string(event.RuleType),
				"cluster_name": event.ClusterName,
				"message":      event.Message,
				"fired_at":     event.FiredAt.Format(time.RFC3339),
			},
		},
	}

	return s.postJSON("https://events.pagerduty.com/v2/enqueue", payload, nil)
}

// sendTeams sends an Adaptive Card to a Microsoft Teams webhook
func (s *AlertService) sendTeams(channel *models.NotificationChannel, event *models.AlertEvent) error {
	webhookURL, ok := channel.Config["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return fmt.Errorf("teams webhook_url not configured")
	}

	payload := map[string]interface{}{
		"type": "message",
		"attachments": []map[string]interface{}{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content": map[string]interface{}{
					"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
					"type":    "AdaptiveCard",
					"version": "1.4",
					"body": []map[string]interface{}{
						{
							"type":   "TextBlock",
							"text":   event.RuleName,
							"size":   "Large",
							"weight": "Bolder",
							"color":  "Attention",
						},
						{
							"type": "TextBlock",
							"text": event.Message,
							"wrap": true,
						},
						{
							"type": "FactSet",
							"facts": []map[string]interface{}{
								{"title": "Severity", "value": string(event.Severity)},
								{"title": "Type", "value": string(event.RuleType)},
								{"title": "Cluster", "value": event.ClusterName},
								{"title": "Fired At", "value": event.FiredAt.Format(time.RFC3339)},
							},
						},
					},
				},
			},
		},
	}

	return s.postJSON(webhookURL, payload, nil)
}

// postJSON sends a JSON POST request to the given URL
func (s *AlertService) postJSON(url string, payload interface{}, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("request returned status %d", resp.StatusCode)
	}

	return nil
}
