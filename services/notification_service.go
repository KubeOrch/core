package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CreateNotification inserts a notification into MongoDB and publishes it via SSE.
func CreateNotification(userID primitive.ObjectID, notifType models.NotificationType, title, message, link, refID string) (*models.Notification, error) {
	now := time.Now()
	notif := &models.Notification{
		ID:        primitive.NewObjectID(),
		UserID:    userID,
		Type:      notifType,
		Title:     title,
		Message:   message,
		Read:      false,
		Link:      link,
		RefID:     refID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := database.NotificationColl.InsertOne(ctx, notif)
	if err != nil {
		logrus.WithError(err).Error("Failed to insert notification")
		return nil, err
	}

	// Publish to SSE broadcaster for real-time delivery
	go publishNotificationSSE(notif)

	return notif, nil
}

func publishNotificationSSE(notif *models.Notification) {
	data, err := json.Marshal(notif)
	if err != nil {
		logrus.WithError(err).Error("Failed to marshal notification for SSE")
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		logrus.WithError(err).Error("Failed to unmarshal notification payload for SSE")
		return
	}

	event := StreamEvent{
		Type:      "notification",
		StreamKey: fmt.Sprintf("notifications:%s", notif.UserID.Hex()),
		EventType: "new_notification",
		Timestamp: notif.CreatedAt,
		Data:      payload,
	}

	GetSSEBroadcaster().Publish(event)
}

// GetNotifications returns paginated notifications for a user, sorted newest-first.
func GetNotifications(userID primitive.ObjectID, limit, offset int) ([]models.Notification, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"user_id": userID}

	total, err := database.NotificationColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64(offset)).
		SetLimit(int64(limit))

	cursor, err := database.NotificationColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var notifications []models.Notification
	if err := cursor.All(ctx, &notifications); err != nil {
		return nil, 0, err
	}

	if notifications == nil {
		notifications = []models.Notification{}
	}

	return notifications, total, nil
}

// GetUnreadCount returns the number of unread notifications for a user.
func GetUnreadCount(userID primitive.ObjectID) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"user_id": userID, "read": false}
	return database.NotificationColl.CountDocuments(ctx, filter)
}

// MarkNotificationRead marks a single notification as read.
func MarkNotificationRead(notifID, userID primitive.ObjectID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": notifID, "user_id": userID}
	update := bson.M{"$set": bson.M{"read": true, "updated_at": time.Now()}}

	result, err := database.NotificationColl.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("notification not found")
	}
	return nil
}

// MarkAllNotificationsRead marks all unread notifications as read for a user.
func MarkAllNotificationsRead(userID primitive.ObjectID) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"user_id": userID, "read": false}
	update := bson.M{"$set": bson.M{"read": true, "updated_at": time.Now()}}

	result, err := database.NotificationColl.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}
