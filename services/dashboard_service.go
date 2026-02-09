package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// currentMonth returns the current month string in "2006-01" format.
func currentMonth() string {
	return time.Now().Format("2006-01")
}

// computeStats runs an aggregation query across all workflows to get platform-wide stats.
func computeStats() (models.StatsSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"deleted_at": nil}}},
		{{Key: "$group", Value: bson.M{
			"_id":                 nil,
			"total_workflows":     bson.M{"$sum": 1},
			"published_workflows": bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$status", "published"}}, 1, 0}}},
			"total_runs":          bson.M{"$sum": "$run_count"},
			"total_success":       bson.M{"$sum": "$success_count"},
		}}},
	}

	cursor, err := database.WorkflowColl.Aggregate(ctx, pipeline)
	if err != nil {
		return models.StatsSnapshot{}, err
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			logrus.WithError(err).Warn("Failed to close cursor")
		}
	}()

	var results []struct {
		TotalWorkflows     int `bson:"total_workflows"`
		PublishedWorkflows int `bson:"published_workflows"`
		TotalRuns          int `bson:"total_runs"`
		TotalSuccess       int `bson:"total_success"`
	}

	if err := cursor.All(ctx, &results); err != nil {
		return models.StatsSnapshot{}, err
	}

	if len(results) == 0 {
		return models.StatsSnapshot{}, nil
	}

	r := results[0]
	var successRate float64
	if r.TotalRuns > 0 {
		successRate = math.Round(float64(r.TotalSuccess)/float64(r.TotalRuns)*1000) / 10 // one decimal
	}

	return models.StatsSnapshot{
		TotalWorkflows:     r.TotalWorkflows,
		PublishedWorkflows: r.PublishedWorkflows,
		TotalRuns:          r.TotalRuns,
		SuccessRate:        successRate,
	}, nil
}

// GetDashboardStats computes current stats and handles month rollover.
// Returns the stats response with change percentages.
func GetDashboardStats() (*models.DashboardStatsResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	month := currentMonth()

	// Compute live stats
	stats, err := computeStats()
	if err != nil {
		return nil, fmt.Errorf("failed to compute stats: %w", err)
	}

	// Load existing dashboard stats doc (there's only ever one)
	var existing models.DashboardStats
	err = database.DashboardStatsColl.FindOne(ctx, bson.M{}).Decode(&existing)

	if err == mongo.ErrNoDocuments {
		// First ever request — create the doc, no previous month data
		doc := models.DashboardStats{
			Month:          month,
			CurrentStats:   stats,
			LastComputedAt: time.Now(),
		}
		_, err := database.DashboardStatsColl.InsertOne(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("failed to insert dashboard stats: %w", err)
		}
		return buildResponse(stats, nil), nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to load dashboard stats: %w", err)
	}

	if existing.Month == month {
		// Same month — update current stats
		update := bson.M{
			"$set": bson.M{
				"current_stats":   stats,
				"last_computed_at": time.Now(),
			},
		}
		_, err := database.DashboardStatsColl.UpdateOne(ctx, bson.M{"_id": existing.ID}, update)
		if err != nil {
			logrus.WithError(err).Warn("Failed to update dashboard stats")
		}
		return buildResponse(stats, existing.PreviousMonthStats), nil
	}

	// New month — roll over: current becomes previous
	update := bson.M{
		"$set": bson.M{
			"month":                month,
			"current_stats":        stats,
			"previous_month_stats": existing.CurrentStats,
			"last_computed_at":     time.Now(),
		},
	}
	opts := options.Update().SetUpsert(true)
	_, err = database.DashboardStatsColl.UpdateOne(ctx, bson.M{"_id": existing.ID}, update, opts)
	if err != nil {
		logrus.WithError(err).Warn("Failed to roll over dashboard stats")
	}

	// After rollover, previous month is the old current stats
	prev := existing.CurrentStats
	return buildResponse(stats, &prev), nil
}

// buildResponse converts raw stats + optional previous month into the API response.
func buildResponse(current models.StatsSnapshot, previous *models.StatsSnapshot) *models.DashboardStatsResponse {
	return &models.DashboardStatsResponse{
		TotalWorkflows:     makeStat(current.TotalWorkflows, intPtr(previous, func(s *models.StatsSnapshot) int { return s.TotalWorkflows })),
		PublishedWorkflows: makeStat(current.PublishedWorkflows, intPtr(previous, func(s *models.StatsSnapshot) int { return s.PublishedWorkflows })),
		TotalRuns:          makeStat(current.TotalRuns, intPtr(previous, func(s *models.StatsSnapshot) int { return s.TotalRuns })),
		SuccessRate:        makeFloatStat(current.SuccessRate, floatPtr(previous, func(s *models.StatsSnapshot) float64 { return s.SuccessRate })),
	}
}

func intPtr(s *models.StatsSnapshot, fn func(*models.StatsSnapshot) int) *int {
	if s == nil {
		return nil
	}
	v := fn(s)
	return &v
}

func floatPtr(s *models.StatsSnapshot, fn func(*models.StatsSnapshot) float64) *float64 {
	if s == nil {
		return nil
	}
	v := fn(s)
	return &v
}

func makeStat(current int, previous *int) models.StatChange {
	sc := models.StatChange{
		Value: fmt.Sprintf("%d", current),
		Trend: "neutral",
	}

	if previous != nil && *previous > 0 {
		sc.Change = math.Round((float64(current)-float64(*previous))/float64(*previous)*1000) / 10
		if sc.Change > 0 {
			sc.Trend = "up"
		} else if sc.Change < 0 {
			sc.Trend = "down"
		}
	}

	return sc
}

func makeFloatStat(current float64, previous *float64) models.StatChange {
	sc := models.StatChange{
		Value: fmt.Sprintf("%.1f%%", current),
		Trend: "neutral",
	}

	if previous != nil && *previous > 0 {
		sc.Change = math.Round((current-*previous)*10) / 10 // absolute difference in percentage points
		if sc.Change > 0 {
			sc.Trend = "up"
		} else if sc.Change < 0 {
			sc.Trend = "down"
		}
	}

	return sc
}
