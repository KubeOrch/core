package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ResourceType represents the type of Kubernetes resource
type ResourceType string

const (
	ResourceTypePod                   ResourceType = "Pod"
	ResourceTypeDeployment            ResourceType = "Deployment"
	ResourceTypeStatefulSet           ResourceType = "StatefulSet"
	ResourceTypeDaemonSet             ResourceType = "DaemonSet"
	ResourceTypeReplicaSet            ResourceType = "ReplicaSet"
	ResourceTypeService               ResourceType = "Service"
	ResourceTypeIngress               ResourceType = "Ingress"
	ResourceTypeConfigMap             ResourceType = "ConfigMap"
	ResourceTypeSecret                ResourceType = "Secret"
	ResourceTypeNamespace             ResourceType = "Namespace"
	ResourceTypeNode                  ResourceType = "Node"
	ResourceTypePersistentVolume      ResourceType = "PersistentVolume"
	ResourceTypePersistentVolumeClaim ResourceType = "PersistentVolumeClaim"
	ResourceTypeJob                   ResourceType = "Job"
	ResourceTypeCronJob               ResourceType = "CronJob"
	ResourceTypeServiceAccount        ResourceType = "ServiceAccount"
	ResourceTypeRole                  ResourceType = "Role"
	ResourceTypeClusterRole           ResourceType = "ClusterRole"
	ResourceTypeHPA                   ResourceType = "HorizontalPodAutoscaler"
	ResourceTypeVPA                   ResourceType = "VerticalPodAutoscaler"
	ResourceTypeNetworkPolicy         ResourceType = "NetworkPolicy"
	ResourceTypeStorageClass          ResourceType = "StorageClass"
)

// ResourceStatus represents the status of a resource
type ResourceStatus string

const (
	ResourceStatusRunning   ResourceStatus = "running"
	ResourceStatusPending   ResourceStatus = "pending"
	ResourceStatusFailed    ResourceStatus = "failed"
	ResourceStatusCompleted ResourceStatus = "completed"
	ResourceStatusUnknown   ResourceStatus = "unknown"
	ResourceStatusDeleted   ResourceStatus = "deleted"
	ResourceStatusWarning   ResourceStatus = "warning"
)

// Resource represents a Kubernetes resource stored in MongoDB
type Resource struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID           primitive.ObjectID `bson:"userId" json:"userId"`
	ClusterID        primitive.ObjectID `bson:"clusterId" json:"clusterId"`
	ClusterName      string             `bson:"clusterName" json:"clusterName"`
	Name             string             `bson:"name" json:"name"`
	Namespace        string             `bson:"namespace" json:"namespace"`
	Type             ResourceType       `bson:"type" json:"type"`
	UID              string             `bson:"uid" json:"uid"`                           // Kubernetes UID
	ResourceVersion  string             `bson:"resourceVersion" json:"resourceVersion"`   // K8s resource version
	Status           ResourceStatus     `bson:"status" json:"status"`
	Labels           map[string]string  `bson:"labels,omitempty" json:"labels,omitempty"`
	Annotations      map[string]string  `bson:"annotations,omitempty" json:"annotations,omitempty"`
	OwnerReferences  []OwnerReference   `bson:"ownerReferences,omitempty" json:"ownerReferences,omitempty"`
	Spec             ResourceSpec       `bson:"spec,omitempty" json:"spec,omitempty"`
	Metadata         ResourceMetadata   `bson:"metadata,omitempty" json:"metadata,omitempty"`
	UserTags         []string           `bson:"userTags,omitempty" json:"userTags,omitempty"`           // User-added tags
	UserNotes        string             `bson:"userNotes,omitempty" json:"userNotes,omitempty"`         // User notes
	IsFavorite       bool               `bson:"isFavorite" json:"isFavorite"`                           // User favorite
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`                             // K8s creation time
	LastSyncedAt     time.Time          `bson:"lastSyncedAt" json:"lastSyncedAt"`                       // Last sync with K8s
	LastSeenAt       time.Time          `bson:"lastSeenAt" json:"lastSeenAt"`                           // Last time resource was seen
	DeletedAt        *time.Time         `bson:"deletedAt,omitempty" json:"deletedAt,omitempty"`         // When resource was deleted
	FirstDiscoveredAt time.Time         `bson:"firstDiscoveredAt" json:"firstDiscoveredAt"`             // When we first saw this resource
}

// OwnerReference contains information about resource ownership
type OwnerReference struct {
	APIVersion string `bson:"apiVersion" json:"apiVersion"`
	Kind       string `bson:"kind" json:"kind"`
	Name       string `bson:"name" json:"name"`
	UID        string `bson:"uid" json:"uid"`
	Controller bool   `bson:"controller" json:"controller"`
}

// ResourceSpec contains type-specific specifications
type ResourceSpec struct {
	// Deployment/StatefulSet/DaemonSet specs
	Replicas          *int32 `bson:"replicas,omitempty" json:"replicas,omitempty"`
	AvailableReplicas *int32 `bson:"availableReplicas,omitempty" json:"availableReplicas,omitempty"`
	ReadyReplicas     *int32 `bson:"readyReplicas,omitempty" json:"readyReplicas,omitempty"`
	UpdatedReplicas   *int32 `bson:"updatedReplicas,omitempty" json:"updatedReplicas,omitempty"`

	// Pod specs
	Containers []ContainerSpec `bson:"containers,omitempty" json:"containers,omitempty"`
	NodeName   string          `bson:"nodeName,omitempty" json:"nodeName,omitempty"`
	PodIP      string          `bson:"podIP,omitempty" json:"podIP,omitempty"`
	HostIP     string          `bson:"hostIP,omitempty" json:"hostIP,omitempty"`

	// Service specs
	ServiceType string   `bson:"serviceType,omitempty" json:"serviceType,omitempty"`
	ClusterIP   string   `bson:"clusterIP,omitempty" json:"clusterIP,omitempty"`
	ExternalIPs []string `bson:"externalIPs,omitempty" json:"externalIPs,omitempty"`
	Ports       []Port   `bson:"ports,omitempty" json:"ports,omitempty"`

	// Node specs
	NodeCapacity  NodeResources `bson:"nodeCapacity,omitempty" json:"nodeCapacity,omitempty"`
	NodeAllocated NodeResources `bson:"nodeAllocated,omitempty" json:"nodeAllocated,omitempty"`

	// PVC specs
	StorageClass *string `bson:"storageClass,omitempty" json:"storageClass,omitempty"`
	AccessModes  []string `bson:"accessModes,omitempty" json:"accessModes,omitempty"`
	Capacity     string  `bson:"capacity,omitempty" json:"capacity,omitempty"`
}

// ContainerSpec represents container specifications
type ContainerSpec struct {
	Name            string            `bson:"name" json:"name"`
	Image           string            `bson:"image" json:"image"`
	Command         []string          `bson:"command,omitempty" json:"command,omitempty"`
	Args            []string          `bson:"args,omitempty" json:"args,omitempty"`
	Ports           []ContainerPort   `bson:"ports,omitempty" json:"ports,omitempty"`
	Resources       ContainerResources `bson:"resources,omitempty" json:"resources,omitempty"`
	RestartCount    int32             `bson:"restartCount" json:"restartCount"`
	Ready           bool              `bson:"ready" json:"ready"`
	State           string            `bson:"state" json:"state"`
	LastState       string            `bson:"lastState,omitempty" json:"lastState,omitempty"`
	TerminationReason string          `bson:"terminationReason,omitempty" json:"terminationReason,omitempty"`
}

// ContainerPort represents a container port
type ContainerPort struct {
	Name          string `bson:"name,omitempty" json:"name,omitempty"`
	ContainerPort int32  `bson:"containerPort" json:"containerPort"`
	Protocol      string `bson:"protocol" json:"protocol"`
}

// ContainerResources represents container resource requests/limits
type ContainerResources struct {
	RequestsCPU    string `bson:"requestsCPU,omitempty" json:"requestsCPU,omitempty"`
	RequestsMemory string `bson:"requestsMemory,omitempty" json:"requestsMemory,omitempty"`
	LimitsCPU      string `bson:"limitsCPU,omitempty" json:"limitsCPU,omitempty"`
	LimitsMemory   string `bson:"limitsMemory,omitempty" json:"limitsMemory,omitempty"`
}

// NodeResources represents node resource information
type NodeResources struct {
	CPU              string `bson:"cpu,omitempty" json:"cpu,omitempty"`
	Memory           string `bson:"memory,omitempty" json:"memory,omitempty"`
	Storage          string `bson:"storage,omitempty" json:"storage,omitempty"`
	Pods             string `bson:"pods,omitempty" json:"pods,omitempty"`
}

// Port represents a service port
type Port struct {
	Name       string `bson:"name,omitempty" json:"name,omitempty"`
	Port       int32  `bson:"port" json:"port"`
	TargetPort int32  `bson:"targetPort,omitempty" json:"targetPort,omitempty"`
	NodePort   int32  `bson:"nodePort,omitempty" json:"nodePort,omitempty"`
	Protocol   string `bson:"protocol" json:"protocol"`
}

// ResourceMetadata contains additional metadata
type ResourceMetadata struct {
	Generation      int64             `bson:"generation,omitempty" json:"generation,omitempty"`
	SelfLink        string            `bson:"selfLink,omitempty" json:"selfLink,omitempty"`
	Finalizers      []string          `bson:"finalizers,omitempty" json:"finalizers,omitempty"`
	ManagedFields   []ManagedField    `bson:"managedFields,omitempty" json:"managedFields,omitempty"`
	Events          []ResourceEvent   `bson:"events,omitempty" json:"events,omitempty"`
}

// ManagedField represents field ownership
type ManagedField struct {
	Manager   string    `bson:"manager" json:"manager"`
	Operation string    `bson:"operation" json:"operation"`
	Time      time.Time `bson:"time" json:"time"`
}

// ResourceEvent represents a Kubernetes event related to this resource
type ResourceEvent struct {
	Type      string    `bson:"type" json:"type"`
	Reason    string    `bson:"reason" json:"reason"`
	Message   string    `bson:"message" json:"message"`
	Count     int32     `bson:"count" json:"count"`
	FirstSeen time.Time `bson:"firstSeen" json:"firstSeen"`
	LastSeen  time.Time `bson:"lastSeen" json:"lastSeen"`
	Source    string    `bson:"source" json:"source"`
}

// ResourceHistory tracks changes to a resource over time
type ResourceHistory struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ResourceID primitive.ObjectID `bson:"resourceId" json:"resourceId"`
	UserID     primitive.ObjectID `bson:"userId" json:"userId"`
	Action     string             `bson:"action" json:"action"` // created, updated, deleted, scaled, restarted
	Changes    map[string]interface{} `bson:"changes,omitempty" json:"changes,omitempty"`
	OldStatus  ResourceStatus     `bson:"oldStatus,omitempty" json:"oldStatus,omitempty"`
	NewStatus  ResourceStatus     `bson:"newStatus,omitempty" json:"newStatus,omitempty"`
	Timestamp  time.Time          `bson:"timestamp" json:"timestamp"`
	Message    string             `bson:"message,omitempty" json:"message,omitempty"`
}

// ResourceAccess tracks who accessed a resource
type ResourceAccess struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ResourceID primitive.ObjectID `bson:"resourceId" json:"resourceId"`
	UserID     primitive.ObjectID `bson:"userId" json:"userId"`
	Action     string             `bson:"action" json:"action"` // viewed, edited, deleted, logs, exec, port-forward
	Timestamp  time.Time          `bson:"timestamp" json:"timestamp"`
	Details    map[string]string  `bson:"details,omitempty" json:"details,omitempty"`
}