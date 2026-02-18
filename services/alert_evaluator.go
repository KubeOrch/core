package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KubeOrch/core/database"
	"github.com/KubeOrch/core/models"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AlertEvaluator struct {
	alertService   *AlertService
	clusterService *KubernetesClusterService
	logger         *logrus.Logger
	interval       time.Duration
	stopChan       chan struct{}
	wg             sync.WaitGroup
	mu             sync.RWMutex
	running        bool
}

func NewAlertEvaluator(interval time.Duration) *AlertEvaluator {
	if interval == 0 {
		interval = 60 * time.Second
	}
	return &AlertEvaluator{
		alertService:   GetAlertService(),
		clusterService: GetKubernetesClusterService(),
		logger:         logrus.New(),
		interval:       interval,
		stopChan:       make(chan struct{}),
	}
}

// Singleton
var (
	alertEvaluatorInstance *AlertEvaluator
	alertEvaluatorOnce    sync.Once
)

func GetAlertEvaluator() *AlertEvaluator {
	alertEvaluatorOnce.Do(func() {
		alertEvaluatorInstance = NewAlertEvaluator(60 * time.Second)
	})
	return alertEvaluatorInstance
}

func (e *AlertEvaluator) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	e.wg.Add(1)
	go e.evaluateLoop()
	e.logger.Infof("Alert evaluator started with interval: %v", e.interval)
}

func (e *AlertEvaluator) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	e.mu.Unlock()

	close(e.stopChan)
	e.wg.Wait()
	e.logger.Info("Alert evaluator stopped")
}

func (e *AlertEvaluator) evaluateLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.evaluateAllRules()
		case <-e.stopChan:
			return
		}
	}
}

func (e *AlertEvaluator) evaluateAllRules() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rules, err := e.alertService.repo.GetEnabledRules(ctx)
	if err != nil {
		e.logger.WithError(err).Error("Failed to get enabled rules for evaluation")
		return
	}

	if len(rules) == 0 {
		return
	}

	// Group by type
	clusterRules := make([]models.AlertRule, 0)
	workflowRules := make([]models.AlertRule, 0)
	resourceRules := make([]models.AlertRule, 0)

	for _, rule := range rules {
		switch rule.Type {
		case models.AlertRuleTypeCluster:
			clusterRules = append(clusterRules, rule)
		case models.AlertRuleTypeWorkflow:
			workflowRules = append(workflowRules, rule)
		case models.AlertRuleTypeResource:
			resourceRules = append(resourceRules, rule)
		}
	}

	var wg sync.WaitGroup
	if len(clusterRules) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.evaluateClusterRules(ctx, clusterRules)
		}()
	}
	if len(workflowRules) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.evaluateWorkflowRules(ctx, workflowRules)
		}()
	}
	if len(resourceRules) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.evaluateResourceRules(ctx, resourceRules)
		}()
	}
	wg.Wait()
}

func (e *AlertEvaluator) evaluateClusterRules(ctx context.Context, rules []models.AlertRule) {
	for _, rule := range rules {
		clusters, err := e.clusterService.ListUserClusters(ctx, rule.UserID)
		if err != nil {
			e.logger.WithError(err).Error("Failed to list clusters for alert evaluation")
			continue
		}

		for _, cluster := range clusters {
			// Check scope filter
			if len(rule.ClusterIDs) > 0 && !contains(rule.ClusterIDs, cluster.Name) {
				continue
			}

			metrics, err := e.clusterService.GetClusterMetrics(ctx, rule.UserID, cluster.Name)
			if err != nil {
				continue
			}

			triggered := false
			var message string

			for _, cond := range rule.Conditions {
				switch cond.Metric {
				case "cpu_percentage":
					if evaluateNumericCondition(metrics.Resources.CPU.Percentage, cond) {
						triggered = true
						message = fmt.Sprintf("Cluster '%s' CPU usage is %.1f%%", cluster.Name, metrics.Resources.CPU.Percentage)
					}
				case "memory_percentage":
					if evaluateNumericCondition(metrics.Resources.Memory.Percentage, cond) {
						triggered = true
						message = fmt.Sprintf("Cluster '%s' memory usage is %.1f%%", cluster.Name, metrics.Resources.Memory.Percentage)
					}
				case "storage_percentage":
					if evaluateNumericCondition(metrics.Resources.Storage.Percentage, cond) {
						triggered = true
						message = fmt.Sprintf("Cluster '%s' storage usage is %.1f%%", cluster.Name, metrics.Resources.Storage.Percentage)
					}
				case "cluster_status":
					if val, ok := cond.Value.(string); ok && string(cluster.Status) == val {
						triggered = true
						message = fmt.Sprintf("Cluster '%s' is %s", cluster.Name, cluster.Status)
					}
				}
			}

			details := map[string]interface{}{
				"cluster_id":   cluster.ID.Hex(),
				"cluster_name": cluster.Name,
			}

			if triggered {
				if err := e.alertService.FireAlert(ctx, &rule, message, details); err != nil {
					e.logger.WithError(err).Error("Failed to fire cluster alert")
				}
			} else {
				// Auto-resolve if condition clears
				if err := e.alertService.ResolveAlertsByRule(ctx, &rule); err != nil {
					e.logger.WithError(err).Error("Failed to resolve cluster alerts")
				}
			}
		}
	}
}

func (e *AlertEvaluator) evaluateWorkflowRules(ctx context.Context, rules []models.AlertRule) {
	for _, rule := range rules {
		for _, cond := range rule.Conditions {
			switch cond.Metric {
			case "run_status":
				e.checkWorkflowRunStatus(ctx, &rule, cond)
			case "consecutive_failures":
				e.checkConsecutiveFailures(ctx, &rule, cond)
			}
		}
	}
}

func (e *AlertEvaluator) checkWorkflowRunStatus(ctx context.Context, rule *models.AlertRule, cond models.AlertCondition) {
	// Look for recent failed runs
	filter := bson.M{"status": "failed"}

	// Check since last evaluation
	since := time.Now().Add(-2 * time.Minute)
	if rule.LastTriggeredAt != nil {
		since = *rule.LastTriggeredAt
	}
	filter["completed_at"] = bson.M{"$gte": since}

	// Scope to specific workflows if set
	if len(rule.WorkflowIDs) > 0 {
		wfIDs := make([]primitive.ObjectID, 0, len(rule.WorkflowIDs))
		for _, id := range rule.WorkflowIDs {
			oid, err := primitive.ObjectIDFromHex(id)
			if err == nil {
				wfIDs = append(wfIDs, oid)
			}
		}
		if len(wfIDs) > 0 {
			filter["workflow_id"] = bson.M{"$in": wfIDs}
		}
	}

	opts := options.Find().SetLimit(10).SetSort(bson.D{{Key: "completed_at", Value: -1}})
	cursor, err := database.WorkflowRunColl.Find(ctx, filter, opts)
	if err != nil {
		return
	}
	defer func() { _ = cursor.Close(ctx) }()

	for cursor.Next(ctx) {
		var run models.WorkflowRun
		if err := cursor.Decode(&run); err != nil {
			continue
		}
		details := map[string]interface{}{
			"workflow_id":   run.WorkflowID.Hex(),
			"workflow_name": run.WorkflowID.Hex(), // Will be resolved by context
			"run_id":        run.ID.Hex(),
		}
		message := fmt.Sprintf("Workflow run failed: %s", run.Error)
		if err := e.alertService.FireAlert(ctx, rule, message, details); err != nil {
			e.logger.WithError(err).Error("Failed to fire workflow alert")
		}
	}
}

func (e *AlertEvaluator) checkConsecutiveFailures(ctx context.Context, rule *models.AlertRule, cond models.AlertCondition) {
	threshold := toFloat64(cond.Value)

	// Get unique workflow IDs from recent runs
	filter := bson.M{}
	if len(rule.WorkflowIDs) > 0 {
		wfIDs := make([]primitive.ObjectID, 0)
		for _, id := range rule.WorkflowIDs {
			oid, err := primitive.ObjectIDFromHex(id)
			if err == nil {
				wfIDs = append(wfIDs, oid)
			}
		}
		filter["workflow_id"] = bson.M{"$in": wfIDs}
	}

	// Get the most recent runs per workflow and check for consecutive failures
	opts := options.Find().SetSort(bson.D{{Key: "completed_at", Value: -1}}).SetLimit(int64(threshold) + 1)
	cursor, err := database.WorkflowRunColl.Find(ctx, filter, opts)
	if err != nil {
		return
	}
	defer func() { _ = cursor.Close(ctx) }()

	consecutiveFailures := 0
	for cursor.Next(ctx) {
		var run models.WorkflowRun
		if err := cursor.Decode(&run); err != nil {
			continue
		}
		if run.Status == models.WorkflowRunStatusFailed {
			consecutiveFailures++
		} else {
			break
		}
	}

	if float64(consecutiveFailures) > threshold {
		details := map[string]interface{}{
			"consecutive_failures": consecutiveFailures,
		}
		message := fmt.Sprintf("Workflow has %d consecutive failures", consecutiveFailures)
		if err := e.alertService.FireAlert(ctx, rule, message, details); err != nil {
			e.logger.WithError(err).Error("Failed to fire consecutive failures alert")
		}
	}
}

func (e *AlertEvaluator) evaluateResourceRules(ctx context.Context, rules []models.AlertRule) {
	// Resource rules are primarily event-driven (pod crashes, OOM, etc.)
	// The ticker-based evaluation handles slower-changing conditions like replica mismatches
	for _, rule := range rules {
		for _, cond := range rule.Conditions {
			switch cond.Metric {
			case "pod_phase":
				// Check for pods stuck in pending state — would need K8s API access
				// This is better handled by event-driven hooks
			case "container_state", "termination_reason":
				// These are event-driven, handled by hooks
			}
		}
	}
}

// EvaluateWorkflowEvent is called by workflow_executor when a run fails (event-driven)
func (e *AlertEvaluator) EvaluateWorkflowEvent(run *models.WorkflowRun) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if run.Status != models.WorkflowRunStatusFailed {
		return
	}

	rules, err := e.alertService.repo.GetEnabledRules(ctx)
	if err != nil {
		e.logger.WithError(err).Error("Failed to get rules for workflow event evaluation")
		return
	}

	for _, rule := range rules {
		if rule.Type != models.AlertRuleTypeWorkflow {
			continue
		}

		// Check scope
		if len(rule.WorkflowIDs) > 0 && !contains(rule.WorkflowIDs, run.WorkflowID.Hex()) {
			continue
		}

		for _, cond := range rule.Conditions {
			if cond.Metric == "run_status" {
				if val, ok := cond.Value.(string); ok && val == string(run.Status) {
					details := map[string]interface{}{
						"workflow_id": run.WorkflowID.Hex(),
						"run_id":      run.ID.Hex(),
					}
					message := fmt.Sprintf("Workflow run failed: %s", run.Error)
					if err := e.alertService.FireAlert(ctx, &rule, message, details); err != nil {
						e.logger.WithError(err).Error("Failed to fire workflow alert")
					}
				}
			}
		}
	}
}

// EvaluateClusterStatusChange is called by cluster_health_monitor on status changes
func (e *AlertEvaluator) EvaluateClusterStatusChange(userID primitive.ObjectID, clusterName string, oldStatus, newStatus models.ClusterStatus) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rules, err := e.alertService.repo.GetEnabledRules(ctx)
	if err != nil {
		e.logger.WithError(err).Error("Failed to get rules for cluster status evaluation")
		return
	}

	for _, rule := range rules {
		if rule.Type != models.AlertRuleTypeCluster || rule.UserID != userID {
			continue
		}

		if len(rule.ClusterIDs) > 0 && !contains(rule.ClusterIDs, clusterName) {
			continue
		}

		for _, cond := range rule.Conditions {
			if cond.Metric == "cluster_status" {
				if val, ok := cond.Value.(string); ok && val == string(newStatus) {
					details := map[string]interface{}{
						"cluster_name": clusterName,
						"old_status":   string(oldStatus),
						"new_status":   string(newStatus),
					}
					message := fmt.Sprintf("Cluster '%s' changed status to %s", clusterName, newStatus)
					if err := e.alertService.FireAlert(ctx, &rule, message, details); err != nil {
						e.logger.WithError(err).Error("Failed to fire cluster status alert")
					}
				} else if string(oldStatus) == val && string(newStatus) != val {
					// Condition cleared, resolve
					if err := e.alertService.ResolveAlertsByRule(ctx, &rule); err != nil {
						e.logger.WithError(err).Error("Failed to resolve cluster status alerts")
					}
				}
			}
		}
	}
}

// --- Helpers ---

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func evaluateNumericCondition(actual float64, cond models.AlertCondition) bool {
	threshold := toFloat64(cond.Value)
	switch cond.Operator {
	case models.AlertConditionGT:
		return actual > threshold
	case models.AlertConditionGTE:
		return actual >= threshold
	case models.AlertConditionLT:
		return actual < threshold
	case models.AlertConditionLTE:
		return actual <= threshold
	case models.AlertConditionEQ:
		return actual == threshold
	case models.AlertConditionNEQ:
		return actual != threshold
	}
	return false
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
