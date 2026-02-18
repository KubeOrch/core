package handlers

import (
	"net/http"

	"github.com/KubeOrch/core/middleware"
	"github.com/KubeOrch/core/services"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type MetricsHandler struct {
	clusterService *services.KubernetesClusterService
	logger         *logrus.Logger
}

func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{
		clusterService: services.GetKubernetesClusterService(),
		logger:         logrus.New(),
	}
}

// GetMetricsOverview returns aggregated metrics across all user clusters
func (h *MetricsHandler) GetMetricsOverview(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	ctx := c.Request.Context()
	clusters, err := h.clusterService.ListUserClusters(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list clusters for metrics")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type clusterMetrics struct {
		ClusterName string  `json:"clusterName"`
		Status      string  `json:"status"`
		CPUUsed     int64   `json:"cpuUsed"`
		CPUCapacity int64   `json:"cpuCapacity"`
		CPUPercent  float64 `json:"cpuPercent"`
		MemUsed     int64   `json:"memUsed"`
		MemCapacity int64   `json:"memCapacity"`
		MemPercent  float64 `json:"memPercent"`
		StorUsed    int64   `json:"storUsed"`
		StorCap     int64   `json:"storCapacity"`
		StorPercent float64 `json:"storPercent"`
		NodeCount   int     `json:"nodeCount"`
		PodCount    int     `json:"podCount"`
	}

	var metricsResults []clusterMetrics
	var totalCPUUsed, totalCPUCap, totalMemUsed, totalMemCap, totalStorUsed, totalStorCap int64
	var totalNodes, totalPods int

	for _, cluster := range clusters {
		m, err := h.clusterService.GetClusterMetrics(ctx, userID, cluster.Name)
		if err != nil {
			continue
		}

		cm := clusterMetrics{
			ClusterName: cluster.Name,
			Status:      string(cluster.Status),
			CPUUsed:     m.Resources.CPU.Used,
			CPUCapacity: m.Resources.CPU.Capacity,
			CPUPercent:  m.Resources.CPU.Percentage,
			MemUsed:     m.Resources.Memory.Used,
			MemCapacity: m.Resources.Memory.Capacity,
			MemPercent:  m.Resources.Memory.Percentage,
			StorUsed:    m.Resources.Storage.Used,
			StorCap:     m.Resources.Storage.Capacity,
			StorPercent: m.Resources.Storage.Percentage,
			NodeCount:   m.NodeCount,
			PodCount:    m.PodCount,
		}
		metricsResults = append(metricsResults, cm)

		totalCPUUsed += m.Resources.CPU.Used
		totalCPUCap += m.Resources.CPU.Capacity
		totalMemUsed += m.Resources.Memory.Used
		totalMemCap += m.Resources.Memory.Capacity
		totalStorUsed += m.Resources.Storage.Used
		totalStorCap += m.Resources.Storage.Capacity
		totalNodes += m.NodeCount
		totalPods += m.PodCount
	}

	if metricsResults == nil {
		metricsResults = []clusterMetrics{}
	}

	cpuPercent := float64(0)
	if totalCPUCap > 0 {
		cpuPercent = float64(totalCPUUsed) / float64(totalCPUCap) * 100
	}
	memPercent := float64(0)
	if totalMemCap > 0 {
		memPercent = float64(totalMemUsed) / float64(totalMemCap) * 100
	}
	storPercent := float64(0)
	if totalStorCap > 0 {
		storPercent = float64(totalStorUsed) / float64(totalStorCap) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"clusters": metricsResults,
		"totals": gin.H{
			"cpuUsed":        totalCPUUsed,
			"cpuCapacity":    totalCPUCap,
			"cpuPercent":     cpuPercent,
			"memoryUsed":     totalMemUsed,
			"memoryCapacity": totalMemCap,
			"memoryPercent":  memPercent,
			"storageUsed":    totalStorUsed,
			"storageCapacity": totalStorCap,
			"storagePercent": storPercent,
			"nodeCount":      totalNodes,
			"podCount":       totalPods,
		},
	})
}

// GetResourceMetrics returns per-resource metrics with filtering
func (h *MetricsHandler) GetResourceMetrics(c *gin.Context) {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	clusterFilter := c.Query("cluster")
	// TODO: use these filters for per-resource metrics
	_ = c.Query("type")
	_ = c.Query("namespace")

	ctx := c.Request.Context()
	clusters, err := h.clusterService.ListUserClusters(ctx, userID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list clusters")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type resourceMetric struct {
		Name        string `json:"name"`
		Namespace   string `json:"namespace"`
		Type        string `json:"type"`
		ClusterName string `json:"clusterName"`
		Status      string `json:"status"`
		Restarts    int    `json:"restarts"`
	}

	var resources []resourceMetric

	for _, cluster := range clusters {
		if clusterFilter != "" && cluster.Name != clusterFilter {
			continue
		}

		// TODO: query per-resource metrics using typeFilter/namespaceFilter
		// For now, return cluster-level summary
		resources = append(resources, resourceMetric{
			Name:        cluster.Name,
			Namespace:   "",
			Type:        "cluster",
			ClusterName: cluster.Name,
			Status:      string(cluster.Status),
			Restarts:    0,
		})
	}

	if resources == nil {
		resources = []resourceMetric{}
	}

	c.JSON(http.StatusOK, gin.H{"resources": resources})
}
