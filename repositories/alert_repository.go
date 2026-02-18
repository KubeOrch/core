package repositories

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AlertRepository struct {
	ruleColl    *mongo.Collection
	eventColl   *mongo.Collection
	channelColl *mongo.Collection
}

func NewAlertRepository() *AlertRepository {
	db := database.GetDB()
	return &AlertRepository{
		ruleColl:    db.Collection("alert_rules"),
		eventColl:   db.Collection("alert_events"),
		channelColl: db.Collection("notification_channels"),
	}
}

// --- Alert Rules ---

func (r *AlertRepository) CreateRule(ctx context.Context, rule *models.AlertRule) error {
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	result, err := r.ruleColl.InsertOne(ctx, rule)
	if err != nil {
		return fmt.Errorf("failed to create alert rule: %w", err)
	}

	rule.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *AlertRepository) GetRuleByID(ctx context.Context, id primitive.ObjectID) (*models.AlertRule, error) {
	var rule models.AlertRule
	err := r.ruleColl.FindOne(ctx, bson.M{"_id": id}).Decode(&rule)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("alert rule not found")
		}
		return nil, fmt.Errorf("failed to get alert rule: %w", err)
	}
	return &rule, nil
}

func (r *AlertRepository) ListRulesByUser(ctx context.Context, userID primitive.ObjectID, ruleType string, severity string, enabled *bool) ([]models.AlertRule, error) {
	filter := bson.M{"user_id": userID}
	if ruleType != "" {
		filter["type"] = ruleType
	}
	if severity != "" {
		filter["severity"] = severity
	}
	if enabled != nil {
		filter["enabled"] = *enabled
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := r.ruleColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert rules: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var rules []models.AlertRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, fmt.Errorf("failed to decode alert rules: %w", err)
	}
	return rules, nil
}

func (r *AlertRepository) GetEnabledRules(ctx context.Context) ([]models.AlertRule, error) {
	cursor, err := r.ruleColl.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled rules: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var rules []models.AlertRule
	if err := cursor.All(ctx, &rules); err != nil {
		return nil, fmt.Errorf("failed to decode enabled rules: %w", err)
	}
	return rules, nil
}

func (r *AlertRepository) UpdateRule(ctx context.Context, rule *models.AlertRule) error {
	rule.UpdatedAt = time.Now()
	update := bson.M{
		"$set": bson.M{
			"name":                     rule.Name,
			"description":              rule.Description,
			"type":                     rule.Type,
			"severity":                 rule.Severity,
			"enabled":                  rule.Enabled,
			"conditions":               rule.Conditions,
			"cluster_ids":              rule.ClusterIDs,
			"workflow_ids":             rule.WorkflowIDs,
			"resource_types":           rule.ResourceTypes,
			"namespaces":               rule.Namespaces,
			"notification_channel_ids": rule.NotificationChannelIDs,
			"evaluation_interval":      rule.EvaluationInterval,
			"cooldown_period":          rule.CooldownPeriod,
			"updated_at":               rule.UpdatedAt,
		},
	}

	result, err := r.ruleColl.UpdateOne(ctx, bson.M{"_id": rule.ID}, update)
	if err != nil {
		return fmt.Errorf("failed to update alert rule: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("alert rule not found")
	}
	return nil
}

func (r *AlertRepository) DeleteRule(ctx context.Context, id primitive.ObjectID) error {
	result, err := r.ruleColl.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete alert rule: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("alert rule not found")
	}
	return nil
}

func (r *AlertRepository) ToggleRule(ctx context.Context, id primitive.ObjectID, enabled bool) error {
	update := bson.M{"$set": bson.M{"enabled": enabled, "updated_at": time.Now()}}
	result, err := r.ruleColl.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to toggle alert rule: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("alert rule not found")
	}
	return nil
}

func (r *AlertRepository) UpdateRuleTrigger(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	update := bson.M{
		"$set": bson.M{"last_triggered_at": now, "updated_at": now},
		"$inc": bson.M{"trigger_count": 1},
	}
	_, err := r.ruleColl.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("failed to update rule trigger: %w", err)
	}
	return nil
}

// --- Alert Events ---

func (r *AlertRepository) CreateEvent(ctx context.Context, event *models.AlertEvent) error {
	result, err := r.eventColl.InsertOne(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to create alert event: %w", err)
	}
	event.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *AlertRepository) ListEventsByUser(ctx context.Context, userID primitive.ObjectID, severity string, status string, ruleType string, page int, limit int) ([]models.AlertEvent, int64, error) {
	filter := bson.M{"user_id": userID}
	if severity != "" {
		filter["severity"] = severity
	}
	if status != "" {
		filter["status"] = status
	}
	if ruleType != "" {
		filter["rule_type"] = ruleType
	}

	total, err := r.eventColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count alert events: %w", err)
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().
		SetSort(bson.D{{Key: "fired_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(limit))

	cursor, err := r.eventColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list alert events: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var events []models.AlertEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, 0, fmt.Errorf("failed to decode alert events: %w", err)
	}
	return events, total, nil
}

func (r *AlertRepository) UpdateEventStatus(ctx context.Context, id primitive.ObjectID, status models.AlertEventStatus, acknowledgedBy *primitive.ObjectID) error {
	update := bson.M{"$set": bson.M{"status": status}}

	now := time.Now()
	switch status {
	case models.AlertEventResolved:
		update["$set"].(bson.M)["resolved_at"] = now
	case models.AlertEventAcknowledged:
		update["$set"].(bson.M)["acknowledged_at"] = now
		if acknowledgedBy != nil {
			update["$set"].(bson.M)["acknowledged_by"] = *acknowledgedBy
		}
	}

	filter := bson.M{"_id": id}
	if acknowledgedBy != nil {
		filter["user_id"] = *acknowledgedBy
	}
	result, err := r.eventColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update alert event status: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("alert event not found")
	}
	return nil
}

func (r *AlertRepository) GetActiveEventsByRule(ctx context.Context, ruleID primitive.ObjectID) ([]models.AlertEvent, error) {
	filter := bson.M{
		"rule_id": ruleID,
		"status":  bson.M{"$in": []string{string(models.AlertEventFiring), string(models.AlertEventAcknowledged)}},
	}
	cursor, err := r.eventColl.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get active events: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var events []models.AlertEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("failed to decode active events: %w", err)
	}
	return events, nil
}

func (r *AlertRepository) CountActiveEventsByUser(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	filter := bson.M{
		"user_id": userID,
		"status":  models.AlertEventFiring,
	}
	count, err := r.eventColl.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to count active events: %w", err)
	}
	return count, nil
}

func (r *AlertRepository) CountEventsBySeverity(ctx context.Context, userID primitive.ObjectID) (map[string]int64, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"user_id": userID, "status": models.AlertEventFiring}}},
		{{Key: "$group", Value: bson.M{"_id": "$severity", "count": bson.M{"$sum": 1}}}},
	}

	cursor, err := r.eventColl.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to count events by severity: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	result := map[string]int64{"critical": 0, "warning": 0, "info": 0}
	for cursor.Next(ctx) {
		var entry struct {
			ID    string `bson:"_id"`
			Count int64  `bson:"count"`
		}
		if err := cursor.Decode(&entry); err != nil {
			continue
		}
		result[entry.ID] = entry.Count
	}
	return result, nil
}

func (r *AlertRepository) ResolveEventsByRule(ctx context.Context, ruleID primitive.ObjectID) error {
	now := time.Now()
	filter := bson.M{
		"rule_id": ruleID,
		"status":  models.AlertEventFiring,
	}
	update := bson.M{
		"$set": bson.M{
			"status":      models.AlertEventResolved,
			"resolved_at": now,
		},
	}
	_, err := r.eventColl.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to resolve events by rule: %w", err)
	}
	return nil
}

// --- Notification Channels ---

func (r *AlertRepository) CreateChannel(ctx context.Context, channel *models.NotificationChannel) error {
	channel.CreatedAt = time.Now()
	channel.UpdatedAt = time.Now()

	result, err := r.channelColl.InsertOne(ctx, channel)
	if err != nil {
		return fmt.Errorf("failed to create notification channel: %w", err)
	}
	channel.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *AlertRepository) GetChannelByID(ctx context.Context, id primitive.ObjectID) (*models.NotificationChannel, error) {
	var channel models.NotificationChannel
	err := r.channelColl.FindOne(ctx, bson.M{"_id": id}).Decode(&channel)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("notification channel not found")
		}
		return nil, fmt.Errorf("failed to get notification channel: %w", err)
	}
	return &channel, nil
}

func (r *AlertRepository) ListChannelsByUser(ctx context.Context, userID primitive.ObjectID) ([]models.NotificationChannel, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := r.channelColl.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list notification channels: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var channels []models.NotificationChannel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, fmt.Errorf("failed to decode notification channels: %w", err)
	}
	return channels, nil
}

func (r *AlertRepository) UpdateChannel(ctx context.Context, channel *models.NotificationChannel) error {
	channel.UpdatedAt = time.Now()
	update := bson.M{
		"$set": bson.M{
			"name":       channel.Name,
			"type":       channel.Type,
			"config":     channel.Config,
			"enabled":    channel.Enabled,
			"updated_at": channel.UpdatedAt,
		},
	}

	result, err := r.channelColl.UpdateOne(ctx, bson.M{"_id": channel.ID}, update)
	if err != nil {
		return fmt.Errorf("failed to update notification channel: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("notification channel not found")
	}
	return nil
}

func (r *AlertRepository) DeleteChannel(ctx context.Context, id primitive.ObjectID) error {
	result, err := r.channelColl.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete notification channel: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("notification channel not found")
	}
	return nil
}

func (r *AlertRepository) GetChannelsByIDs(ctx context.Context, ids []primitive.ObjectID) ([]models.NotificationChannel, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	filter := bson.M{"_id": bson.M{"$in": ids}, "enabled": true}
	cursor, err := r.channelColl.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get channels by IDs: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var channels []models.NotificationChannel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, fmt.Errorf("failed to decode channels: %w", err)
	}
	return channels, nil
}
