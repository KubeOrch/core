package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// StatsSnapshot holds the computed platform-wide statistics
type StatsSnapshot struct {
	TotalWorkflows     int     `json:"total_workflows" bson:"total_workflows"`
	PublishedWorkflows int     `json:"published_workflows" bson:"published_workflows"`
	TotalRuns          int     `json:"total_runs" bson:"total_runs"`
	SuccessRate        float64 `json:"success_rate" bson:"success_rate"` // 0-100 percentage
}

// DashboardStats is a single document that tracks platform-wide stats
// with month-over-month comparison via lazy snapshot rollover.
type DashboardStats struct {
	ID                 primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	Month              string            `json:"month" bson:"month"` // "2026-02" format
	CurrentStats       StatsSnapshot     `json:"current_stats" bson:"current_stats"`
	PreviousMonthStats *StatsSnapshot    `json:"previous_month_stats,omitempty" bson:"previous_month_stats,omitempty"`
	LastComputedAt     time.Time         `json:"last_computed_at" bson:"last_computed_at"`
}

// StatChange represents a single stat card with its change from last month
type StatChange struct {
	Value  string  `json:"value"`
	Change float64 `json:"change"` // percentage change, e.g. +12.5 or -3.0
	Trend  string  `json:"trend"`  // "up", "down", or "neutral"
}

// DashboardStatsResponse is the API response shape
type DashboardStatsResponse struct {
	TotalWorkflows     StatChange `json:"total_workflows"`
	PublishedWorkflows StatChange `json:"published_workflows"`
	TotalRuns          StatChange `json:"total_runs"`
	SuccessRate        StatChange `json:"success_rate"`
}
