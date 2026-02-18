package repositories

import (
	"fmt"
	"strings"

	"github.com/KubeOrch/core/models"
)

// DiffResult holds the computed diff between two resource states.
type DiffResult struct {
	Action  string
	Message string
	Changes map[string]interface{}
}

// noisyAnnotations are annotation keys that should be ignored during diffing.
var noisyAnnotations = map[string]bool{
	"kubectl.kubernetes.io/last-applied-configuration": true,
	"deployment.kubernetes.io/revision":                true,
}

// ComputeResourceDiff compares two resource states and returns tracked changes.
// Returns nil if no meaningful changes are detected.
func ComputeResourceDiff(existing, incoming *models.Resource) *DiffResult {
	changes := make(map[string]interface{})

	// Universal fields
	diffStringMaps(changes, "labels", existing.Labels, incoming.Labels)
	diffAnnotations(changes, existing.Annotations, incoming.Annotations)

	// Type-specific fields
	switch incoming.Type {
	case models.ResourceTypeDeployment, models.ResourceTypeStatefulSet, models.ResourceTypeReplicaSet:
		diffInt32Ptr(changes, "replicas", existing.Spec.Replicas, incoming.Spec.Replicas)
		diffInt32Ptr(changes, "availableReplicas", existing.Spec.AvailableReplicas, incoming.Spec.AvailableReplicas)
		diffInt32Ptr(changes, "readyReplicas", existing.Spec.ReadyReplicas, incoming.Spec.ReadyReplicas)
		diffContainers(changes, existing.Spec.Containers, incoming.Spec.Containers)

	case models.ResourceTypeDaemonSet:
		diffInt32(changes, "desiredNumberScheduled", existing.Spec.DesiredNumberScheduled, incoming.Spec.DesiredNumberScheduled)
		diffInt32(changes, "currentNumberScheduled", existing.Spec.CurrentNumberScheduled, incoming.Spec.CurrentNumberScheduled)
		diffInt32(changes, "numberReady", existing.Spec.NumberReady, incoming.Spec.NumberReady)
		diffContainers(changes, existing.Spec.Containers, incoming.Spec.Containers)

	case models.ResourceTypePod:
		diffContainers(changes, existing.Spec.Containers, incoming.Spec.Containers)
		diffString(changes, "nodeName", existing.Spec.NodeName, incoming.Spec.NodeName)
		diffString(changes, "podIP", existing.Spec.PodIP, incoming.Spec.PodIP)

	case models.ResourceTypeService:
		diffString(changes, "serviceType", existing.Spec.ServiceType, incoming.Spec.ServiceType)
		diffString(changes, "clusterIP", existing.Spec.ClusterIP, incoming.Spec.ClusterIP)
		diffPorts(changes, existing.Spec.Ports, incoming.Spec.Ports)

	case models.ResourceTypeJob:
		diffInt32(changes, "jobSucceeded", existing.Spec.JobSucceeded, incoming.Spec.JobSucceeded)
		diffInt32(changes, "jobFailed", existing.Spec.JobFailed, incoming.Spec.JobFailed)
		diffInt32(changes, "jobActive", existing.Spec.JobActive, incoming.Spec.JobActive)
		if existing.Spec.CompletionTime == nil && incoming.Spec.CompletionTime != nil {
			changes["completionTime"] = map[string]interface{}{
				"old": nil,
				"new": incoming.Spec.CompletionTime.String(),
			}
		}

	case models.ResourceTypeCronJob:
		diffString(changes, "schedule", existing.Spec.Schedule, incoming.Spec.Schedule)
		diffBoolPtr(changes, "suspend", existing.Spec.Suspend, incoming.Spec.Suspend)
		if existing.Spec.LastScheduleTime == nil && incoming.Spec.LastScheduleTime != nil {
			changes["lastScheduleTime"] = map[string]interface{}{
				"old": nil,
				"new": incoming.Spec.LastScheduleTime.String(),
			}
		} else if existing.Spec.LastScheduleTime != nil && incoming.Spec.LastScheduleTime != nil &&
			!existing.Spec.LastScheduleTime.Equal(*incoming.Spec.LastScheduleTime) {
			changes["lastScheduleTime"] = map[string]interface{}{
				"old": existing.Spec.LastScheduleTime.String(),
				"new": incoming.Spec.LastScheduleTime.String(),
			}
		}

	case models.ResourceTypeHPA:
		diffInt32Ptr(changes, "minReplicas", existing.Spec.MinReplicas, incoming.Spec.MinReplicas)
		diffInt32(changes, "maxReplicas", existing.Spec.MaxReplicas, incoming.Spec.MaxReplicas)
		diffInt32(changes, "currentReplicas", existing.Spec.CurrentReplicas, incoming.Spec.CurrentReplicas)
		diffInt32(changes, "desiredReplicas", existing.Spec.DesiredReplicas, incoming.Spec.DesiredReplicas)
	}

	if len(changes) == 0 {
		return nil
	}

	action := classifyAction(changes)
	message := buildMessage(action, changes, incoming)

	return &DiffResult{
		Action:  action,
		Message: message,
		Changes: changes,
	}
}

// BuildCreationSnapshot captures key initial values for creation events.
func BuildCreationSnapshot(resource *models.Resource) map[string]interface{} {
	snap := map[string]interface{}{
		"type":      string(resource.Type),
		"namespace": resource.Namespace,
		"status":    string(resource.Status),
	}

	switch resource.Type {
	case models.ResourceTypeDeployment, models.ResourceTypeStatefulSet, models.ResourceTypeReplicaSet:
		if resource.Spec.Replicas != nil {
			snap["replicas"] = *resource.Spec.Replicas
		}
		if len(resource.Spec.Containers) > 0 {
			images := make([]string, 0, len(resource.Spec.Containers))
			for _, c := range resource.Spec.Containers {
				images = append(images, c.Image)
			}
			snap["images"] = images
		}

	case models.ResourceTypePod:
		if len(resource.Spec.Containers) > 0 {
			images := make([]string, 0, len(resource.Spec.Containers))
			for _, c := range resource.Spec.Containers {
				images = append(images, c.Image)
			}
			snap["images"] = images
		}
		if resource.Spec.NodeName != "" {
			snap["nodeName"] = resource.Spec.NodeName
		}

	case models.ResourceTypeService:
		if resource.Spec.ServiceType != "" {
			snap["serviceType"] = resource.Spec.ServiceType
		}
		if resource.Spec.ClusterIP != "" {
			snap["clusterIP"] = resource.Spec.ClusterIP
		}
		if len(resource.Spec.Ports) > 0 {
			ports := make([]string, 0, len(resource.Spec.Ports))
			for _, p := range resource.Spec.Ports {
				ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
			}
			snap["ports"] = ports
		}

	case models.ResourceTypeJob:
		if resource.Spec.Completions != nil {
			snap["completions"] = *resource.Spec.Completions
		}

	case models.ResourceTypeCronJob:
		if resource.Spec.Schedule != "" {
			snap["schedule"] = resource.Spec.Schedule
		}

	case models.ResourceTypeHPA:
		if resource.Spec.MinReplicas != nil {
			snap["minReplicas"] = *resource.Spec.MinReplicas
		}
		if resource.Spec.MaxReplicas > 0 {
			snap["maxReplicas"] = resource.Spec.MaxReplicas
		}
		if resource.Spec.ScaleTargetRef != "" {
			snap["scaleTargetRef"] = resource.Spec.ScaleTargetRef
		}
	}

	return snap
}

// BuildCreationMessage returns a descriptive message for resource creation.
func BuildCreationMessage(resource *models.Resource) string {
	if resource.Namespace != "" {
		return fmt.Sprintf("%s discovered in namespace %s", resource.Type, resource.Namespace)
	}
	return fmt.Sprintf("%s discovered", resource.Type)
}

// --- helper functions ---

func diffString(changes map[string]interface{}, field, old, new string) {
	if old != new {
		changes[field] = map[string]interface{}{"old": old, "new": new}
	}
}

func diffInt32(changes map[string]interface{}, field string, old, new int32) {
	if old != new {
		changes[field] = map[string]interface{}{"old": old, "new": new}
	}
}

func diffInt32Ptr(changes map[string]interface{}, field string, old, new *int32) {
	oldVal := int32PtrVal(old)
	newVal := int32PtrVal(new)
	if oldVal != newVal {
		changes[field] = map[string]interface{}{"old": oldVal, "new": newVal}
	}
}

func diffBoolPtr(changes map[string]interface{}, field string, old, new *bool) {
	oldVal := boolPtrVal(old)
	newVal := boolPtrVal(new)
	if oldVal != newVal {
		changes[field] = map[string]interface{}{"old": oldVal, "new": newVal}
	}
}

func int32PtrVal(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func boolPtrVal(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func diffStringMaps(changes map[string]interface{}, field string, old, new map[string]string) {
	added := map[string]string{}
	removed := map[string]string{}
	changed := map[string]interface{}{}

	for k, v := range new {
		if oldV, exists := old[k]; !exists {
			added[k] = v
		} else if oldV != v {
			changed[k] = map[string]interface{}{"old": oldV, "new": v}
		}
	}
	for k, v := range old {
		if _, exists := new[k]; !exists {
			removed[k] = v
		}
	}

	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		return
	}

	diff := map[string]interface{}{}
	if len(added) > 0 {
		diff["added"] = added
	}
	if len(removed) > 0 {
		diff["removed"] = removed
	}
	if len(changed) > 0 {
		diff["changed"] = changed
	}
	changes[field] = diff
}

func diffAnnotations(changes map[string]interface{}, old, new map[string]string) {
	filteredOld := filterAnnotations(old)
	filteredNew := filterAnnotations(new)
	diffStringMaps(changes, "annotations", filteredOld, filteredNew)
}

func filterAnnotations(annotations map[string]string) map[string]string {
	if annotations == nil {
		return nil
	}
	filtered := make(map[string]string, len(annotations))
	for k, v := range annotations {
		if !noisyAnnotations[k] {
			filtered[k] = v
		}
	}
	return filtered
}

func diffContainers(changes map[string]interface{}, old, new []models.ContainerSpec) {
	oldMap := containerMap(old)
	newMap := containerMap(new)

	for name, newC := range newMap {
		oldC, exists := oldMap[name]
		if !exists {
			continue
		}
		prefix := "container." + name
		if oldC.Image != newC.Image {
			changes[prefix+".image"] = map[string]interface{}{"old": oldC.Image, "new": newC.Image}
		}
		if oldC.RestartCount != newC.RestartCount {
			changes[prefix+".restartCount"] = map[string]interface{}{"old": oldC.RestartCount, "new": newC.RestartCount}
		}
		if oldC.State != newC.State {
			changes[prefix+".state"] = map[string]interface{}{"old": oldC.State, "new": newC.State}
		}
		if oldC.Ready != newC.Ready {
			changes[prefix+".ready"] = map[string]interface{}{"old": oldC.Ready, "new": newC.Ready}
		}
	}
}

func containerMap(containers []models.ContainerSpec) map[string]models.ContainerSpec {
	m := make(map[string]models.ContainerSpec, len(containers))
	for _, c := range containers {
		m[c.Name] = c
	}
	return m
}

func diffPorts(changes map[string]interface{}, old, new []models.Port) {
	oldStr := portsToString(old)
	newStr := portsToString(new)
	if oldStr != newStr {
		changes["ports"] = map[string]interface{}{"old": oldStr, "new": newStr}
	}
}

func portsToString(ports []models.Port) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d:%d/%s", p.Port, p.TargetPort, p.Protocol))
	}
	return strings.Join(parts, ",")
}

func classifyAction(changes map[string]interface{}) string {
	hasReplicas := false
	hasImage := false
	hasLabels := false
	hasRestartCount := false
	otherCount := 0

	for k := range changes {
		switch {
		case k == "replicas" || k == "availableReplicas" || k == "readyReplicas" ||
			k == "currentReplicas" || k == "desiredReplicas" || k == "minReplicas" || k == "maxReplicas":
			hasReplicas = true
		case strings.HasSuffix(k, ".image"):
			hasImage = true
		case k == "labels":
			hasLabels = true
		case strings.HasSuffix(k, ".restartCount"):
			hasRestartCount = true
		default:
			otherCount++
		}
	}

	total := 0
	if hasReplicas {
		total++
	}
	if hasImage {
		total++
	}
	if hasLabels {
		total++
	}
	if hasRestartCount {
		total++
	}
	total += otherCount

	if total == 0 {
		return "updated"
	}

	// Single-category changes get specific actions
	if total == 1 || (hasReplicas && !hasImage && !hasLabels && !hasRestartCount && otherCount == 0) {
		if hasReplicas {
			return "scaled"
		}
	}
	if hasImage && !hasReplicas && !hasLabels && !hasRestartCount && otherCount == 0 {
		return "image_changed"
	}
	if hasLabels && !hasReplicas && !hasImage && !hasRestartCount && otherCount == 0 {
		return "labels_changed"
	}
	if hasRestartCount && !hasReplicas && !hasImage && !hasLabels && otherCount == 0 {
		return "container_restarted"
	}

	return "updated"
}

func buildMessage(action string, changes map[string]interface{}, resource *models.Resource) string {
	switch action {
	case "scaled":
		if v, ok := changes["replicas"]; ok {
			if m, ok := v.(map[string]interface{}); ok {
				return fmt.Sprintf("Scaled from %v to %v replicas", m["old"], m["new"])
			}
		}
		return "Replica count changed"
	case "image_changed":
		for k, v := range changes {
			if strings.HasSuffix(k, ".image") {
				if m, ok := v.(map[string]interface{}); ok {
					return fmt.Sprintf("Image changed from %v to %v", m["old"], m["new"])
				}
			}
		}
		return "Container image changed"
	case "labels_changed":
		return "Labels updated"
	case "container_restarted":
		for k, v := range changes {
			if strings.HasSuffix(k, ".restartCount") {
				name := strings.TrimSuffix(strings.TrimPrefix(k, "container."), ".restartCount")
				if m, ok := v.(map[string]interface{}); ok {
					return fmt.Sprintf("Container %s restarted (count: %v → %v)", name, m["old"], m["new"])
				}
			}
		}
		return "Container restarted"
	default:
		return fmt.Sprintf("%s updated with %d field changes", resource.Type, len(changes))
	}
}
