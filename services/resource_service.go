package services

import (
	"context"
	"fmt"
	"time"

	"github.com/KubeOrch/core/models"
	"github.com/KubeOrch/core/repositories"
	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ResourceService struct {
	resourceRepo   *repositories.ResourceRepository
	clusterService *KubernetesClusterService
	logger         *logrus.Logger
}

func NewResourceService() *ResourceService {
	return &ResourceService{
		resourceRepo:   repositories.NewResourceRepository(),
		clusterService: GetKubernetesClusterService(),
		logger:         logrus.New(),
	}
}

var resourceServiceInstance *ResourceService

func GetResourceService() *ResourceService {
	if resourceServiceInstance == nil {
		resourceServiceInstance = NewResourceService()
	}
	return resourceServiceInstance
}

// SyncClusterResources syncs all resources from a cluster to the database
func (s *ResourceService) SyncClusterResources(ctx context.Context, userID primitive.ObjectID, cluster *models.Cluster) error {
	syncStartTime := time.Now()

	clientset, err := s.clusterService.CreateClusterConnection(cluster)
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Sync all namespaces first
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w", err)
	}

	for _, ns := range namespaces.Items {
		// Sync resources in each namespace
		if err := s.syncNamespaceResources(ctx, userID, cluster, clientset, ns.Name); err != nil {
			s.logger.WithError(err).Warnf("Failed to sync namespace %s", ns.Name)
		}
	}

	// Sync cluster-wide resources
	if err := s.syncClusterWideResources(ctx, userID, cluster, clientset); err != nil {
		s.logger.WithError(err).Warn("Failed to sync cluster-wide resources")
	}

	// Mark resources as deleted if they weren't seen in this sync
	if err := s.resourceRepo.MarkDeleted(ctx, userID, cluster.ID, syncStartTime); err != nil {
		s.logger.WithError(err).Warn("Failed to mark deleted resources")
	}

	return nil
}

func (s *ResourceService) syncNamespaceResources(ctx context.Context, userID primitive.ObjectID, cluster *models.Cluster, clientset *kubernetes.Clientset, namespace string) error {
	// Sync Deployments
	deployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list deployments in namespace %s", namespace)
	} else {
		for _, deployment := range deployments.Items {
			resource := s.deploymentToResource(&deployment, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync deployment %s/%s", namespace, deployment.Name)
			}
		}
	}

	// Sync Pods
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list pods in namespace %s", namespace)
	} else {
		for _, pod := range pods.Items {
			resource := s.podToResource(&pod, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync pod %s/%s", namespace, pod.Name)
			}
		}
	}

	// Sync Services
	services, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list services in namespace %s", namespace)
	} else {
		for _, service := range services.Items {
			resource := s.serviceToResource(&service, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync service %s/%s", namespace, service.Name)
			}
		}
	}

	// Sync StatefulSets
	statefulsets, err := clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list statefulsets in namespace %s", namespace)
	} else {
		for _, statefulset := range statefulsets.Items {
			resource := s.statefulSetToResource(&statefulset, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync statefulset %s/%s", namespace, statefulset.Name)
			}
		}
	}

	// Sync ConfigMaps
	configmaps, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list configmaps in namespace %s", namespace)
	} else {
		for _, configmap := range configmaps.Items {
			resource := s.configMapToResource(&configmap, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync configmap %s/%s", namespace, configmap.Name)
			}
		}
	}

	// Sync Secrets
	secrets, err := clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list secrets in namespace %s", namespace)
	} else {
		for _, secret := range secrets.Items {
			// Skip service account tokens
			if secret.Type == corev1.SecretTypeServiceAccountToken {
				continue
			}
			resource := s.secretToResource(&secret, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync secret %s/%s", namespace, secret.Name)
			}
		}
	}

	// Sync Ingresses
	ingresses, err := clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warnf("Failed to list ingresses in namespace %s", namespace)
	} else {
		for _, ingress := range ingresses.Items {
			resource := s.ingressToResource(&ingress, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync ingress %s/%s", namespace, ingress.Name)
			}
		}
	}

	return nil
}

func (s *ResourceService) syncClusterWideResources(ctx context.Context, userID primitive.ObjectID, cluster *models.Cluster, clientset *kubernetes.Clientset) error {
	// Sync Nodes
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warn("Failed to list nodes")
	} else {
		for _, node := range nodes.Items {
			resource := s.nodeToResource(&node, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync node %s", node.Name)
			}
		}
	}

	// Sync Namespaces as resources too
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		s.logger.WithError(err).Warn("Failed to list namespaces")
	} else {
		for _, namespace := range namespaces.Items {
			resource := s.namespaceToResource(&namespace, cluster, userID)
			if err := s.resourceRepo.CreateOrUpdate(ctx, resource); err != nil {
				s.logger.WithError(err).Warnf("Failed to sync namespace %s", namespace.Name)
			}
		}
	}

	return nil
}

// Conversion functions
func (s *ResourceService) deploymentToResource(deployment *appsv1.Deployment, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	var status models.ResourceStatus
	if deployment.Status.Replicas == 0 {
		status = models.ResourceStatusPending
	} else if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
		status = models.ResourceStatusRunning
	} else if deployment.Status.AvailableReplicas > 0 {
		status = models.ResourceStatusWarning
	} else {
		status = models.ResourceStatusFailed
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            deployment.Name,
		Namespace:       deployment.Namespace,
		Type:            models.ResourceTypeDeployment,
		UID:             string(deployment.UID),
		ResourceVersion: deployment.ResourceVersion,
		Status:          status,
		Labels:          deployment.Labels,
		Annotations:     deployment.Annotations,
		CreatedAt:       deployment.CreationTimestamp.Time,
		Spec: models.ResourceSpec{
			Replicas:          deployment.Spec.Replicas,
			AvailableReplicas: &deployment.Status.AvailableReplicas,
			ReadyReplicas:     &deployment.Status.ReadyReplicas,
			UpdatedReplicas:   &deployment.Status.UpdatedReplicas,
		},
	}
}

func (s *ResourceService) podToResource(pod *corev1.Pod, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	status := models.ResourceStatusUnknown
	switch pod.Status.Phase {
	case corev1.PodRunning:
		status = models.ResourceStatusRunning
	case corev1.PodPending:
		status = models.ResourceStatusPending
	case corev1.PodSucceeded:
		status = models.ResourceStatusCompleted
	case corev1.PodFailed:
		status = models.ResourceStatusFailed
	}

	// Convert owner references
	var ownerRefs []models.OwnerReference
	for _, owner := range pod.OwnerReferences {
		ownerRefs = append(ownerRefs, models.OwnerReference{
			APIVersion: owner.APIVersion,
			Kind:       owner.Kind,
			Name:       owner.Name,
			UID:        string(owner.UID),
			Controller: owner.Controller != nil && *owner.Controller,
		})
	}

	// Convert containers
	var containers []models.ContainerSpec
	for _, container := range pod.Spec.Containers {
		containerSpec := models.ContainerSpec{
			Name:    container.Name,
			Image:   container.Image,
			Command: container.Command,
			Args:    container.Args,
		}

		// Add resource requests/limits
		if container.Resources.Requests != nil {
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				containerSpec.Resources.RequestsCPU = cpu.String()
			}
			if memory := container.Resources.Requests.Memory(); memory != nil {
				containerSpec.Resources.RequestsMemory = memory.String()
			}
		}
		if container.Resources.Limits != nil {
			if cpu := container.Resources.Limits.Cpu(); cpu != nil {
				containerSpec.Resources.LimitsCPU = cpu.String()
			}
			if memory := container.Resources.Limits.Memory(); memory != nil {
				containerSpec.Resources.LimitsMemory = memory.String()
			}
		}

		// Add container status
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name {
				containerSpec.RestartCount = status.RestartCount
				containerSpec.Ready = status.Ready
				if status.State.Running != nil {
					containerSpec.State = "running"
				} else if status.State.Waiting != nil {
					containerSpec.State = fmt.Sprintf("waiting: %s", status.State.Waiting.Reason)
				} else if status.State.Terminated != nil {
					containerSpec.State = fmt.Sprintf("terminated: %s", status.State.Terminated.Reason)
					containerSpec.TerminationReason = status.State.Terminated.Reason
				}
			}
		}

		containers = append(containers, containerSpec)
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            pod.Name,
		Namespace:       pod.Namespace,
		Type:            models.ResourceTypePod,
		UID:             string(pod.UID),
		ResourceVersion: pod.ResourceVersion,
		Status:          status,
		Labels:          pod.Labels,
		Annotations:     pod.Annotations,
		OwnerReferences: ownerRefs,
		CreatedAt:       pod.CreationTimestamp.Time,
		Spec: models.ResourceSpec{
			Containers: containers,
			NodeName:   pod.Spec.NodeName,
			PodIP:      pod.Status.PodIP,
			HostIP:     pod.Status.HostIP,
		},
	}
}

func (s *ResourceService) serviceToResource(service *corev1.Service, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	var ports []models.Port
	for _, port := range service.Spec.Ports {
		ports = append(ports, models.Port{
			Name:       port.Name,
			Port:       port.Port,
			TargetPort: port.TargetPort.IntVal,
			NodePort:   port.NodePort,
			Protocol:   string(port.Protocol),
		})
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            service.Name,
		Namespace:       service.Namespace,
		Type:            models.ResourceTypeService,
		UID:             string(service.UID),
		ResourceVersion: service.ResourceVersion,
		Status:          models.ResourceStatusRunning,
		Labels:          service.Labels,
		Annotations:     service.Annotations,
		CreatedAt:       service.CreationTimestamp.Time,
		Spec: models.ResourceSpec{
			ServiceType: string(service.Spec.Type),
			ClusterIP:   service.Spec.ClusterIP,
			ExternalIPs: service.Spec.ExternalIPs,
			Ports:       ports,
		},
	}
}

func (s *ResourceService) statefulSetToResource(statefulset *appsv1.StatefulSet, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	var status models.ResourceStatus
	if statefulset.Status.Replicas == 0 {
		status = models.ResourceStatusPending
	} else if statefulset.Status.ReadyReplicas == *statefulset.Spec.Replicas {
		status = models.ResourceStatusRunning
	} else if statefulset.Status.ReadyReplicas > 0 {
		status = models.ResourceStatusWarning
	} else {
		status = models.ResourceStatusFailed
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            statefulset.Name,
		Namespace:       statefulset.Namespace,
		Type:            models.ResourceTypeStatefulSet,
		UID:             string(statefulset.UID),
		ResourceVersion: statefulset.ResourceVersion,
		Status:          status,
		Labels:          statefulset.Labels,
		Annotations:     statefulset.Annotations,
		CreatedAt:       statefulset.CreationTimestamp.Time,
		Spec: models.ResourceSpec{
			Replicas:          statefulset.Spec.Replicas,
			ReadyReplicas:     &statefulset.Status.ReadyReplicas,
			UpdatedReplicas:   &statefulset.Status.UpdatedReplicas,
		},
	}
}

func (s *ResourceService) configMapToResource(configmap *corev1.ConfigMap, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            configmap.Name,
		Namespace:       configmap.Namespace,
		Type:            models.ResourceTypeConfigMap,
		UID:             string(configmap.UID),
		ResourceVersion: configmap.ResourceVersion,
		Status:          models.ResourceStatusRunning,
		Labels:          configmap.Labels,
		Annotations:     configmap.Annotations,
		CreatedAt:       configmap.CreationTimestamp.Time,
	}
}

func (s *ResourceService) secretToResource(secret *corev1.Secret, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            secret.Name,
		Namespace:       secret.Namespace,
		Type:            models.ResourceTypeSecret,
		UID:             string(secret.UID),
		ResourceVersion: secret.ResourceVersion,
		Status:          models.ResourceStatusRunning,
		Labels:          secret.Labels,
		Annotations:     secret.Annotations,
		CreatedAt:       secret.CreationTimestamp.Time,
	}
}

func (s *ResourceService) ingressToResource(ingress *networkingv1.Ingress, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	// Determine status based on LoadBalancer IP assignment
	status := models.ResourceStatusPending
	var loadBalancerIP string
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		status = models.ResourceStatusRunning
		if ingress.Status.LoadBalancer.Ingress[0].IP != "" {
			loadBalancerIP = ingress.Status.LoadBalancer.Ingress[0].IP
		} else if ingress.Status.LoadBalancer.Ingress[0].Hostname != "" {
			loadBalancerIP = ingress.Status.LoadBalancer.Ingress[0].Hostname
		}
	}

	// Count rules and paths
	rulesCount := len(ingress.Spec.Rules)
	var pathsCount int
	var hosts []string
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
		if rule.HTTP != nil {
			pathsCount += len(rule.HTTP.Paths)
		}
	}

	// Get ingress class
	var ingressClass string
	if ingress.Spec.IngressClassName != nil {
		ingressClass = *ingress.Spec.IngressClassName
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            ingress.Name,
		Namespace:       ingress.Namespace,
		Type:            models.ResourceTypeIngress,
		UID:             string(ingress.UID),
		ResourceVersion: ingress.ResourceVersion,
		Status:          status,
		Labels:          ingress.Labels,
		Annotations:     ingress.Annotations,
		CreatedAt:       ingress.CreationTimestamp.Time,
		Spec: models.ResourceSpec{
			IngressClass:   ingressClass,
			IngressHosts:   hosts,
			IngressRules:   rulesCount,
			IngressPaths:   pathsCount,
			LoadBalancerIP: loadBalancerIP,
		},
	}
}

func (s *ResourceService) nodeToResource(node *corev1.Node, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	status := models.ResourceStatusUnknown
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				status = models.ResourceStatusRunning
			} else {
				status = models.ResourceStatusFailed
			}
			break
		}
	}

	nodeCapacity := models.NodeResources{}
	nodeAllocated := models.NodeResources{}

	if node.Status.Capacity != nil {
		if cpu := node.Status.Capacity.Cpu(); cpu != nil {
			nodeCapacity.CPU = cpu.String()
		}
		if memory := node.Status.Capacity.Memory(); memory != nil {
			nodeCapacity.Memory = memory.String()
		}
		if storage := node.Status.Capacity.Storage(); storage != nil {
			nodeCapacity.Storage = storage.String()
		}
		if pods := node.Status.Capacity.Pods(); pods != nil {
			nodeCapacity.Pods = pods.String()
		}
	}

	if node.Status.Allocatable != nil {
		if cpu := node.Status.Allocatable.Cpu(); cpu != nil {
			nodeAllocated.CPU = cpu.String()
		}
		if memory := node.Status.Allocatable.Memory(); memory != nil {
			nodeAllocated.Memory = memory.String()
		}
		if storage := node.Status.Allocatable.Storage(); storage != nil {
			nodeAllocated.Storage = storage.String()
		}
		if pods := node.Status.Allocatable.Pods(); pods != nil {
			nodeAllocated.Pods = pods.String()
		}
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            node.Name,
		Namespace:       "",
		Type:            models.ResourceTypeNode,
		UID:             string(node.UID),
		ResourceVersion: node.ResourceVersion,
		Status:          status,
		Labels:          node.Labels,
		Annotations:     node.Annotations,
		CreatedAt:       node.CreationTimestamp.Time,
		Spec: models.ResourceSpec{
			NodeCapacity:  nodeCapacity,
			NodeAllocated: nodeAllocated,
		},
	}
}

func (s *ResourceService) namespaceToResource(namespace *corev1.Namespace, cluster *models.Cluster, userID primitive.ObjectID) *models.Resource {
	status := models.ResourceStatusRunning
	if namespace.Status.Phase == corev1.NamespaceTerminating {
		status = models.ResourceStatusDeleted
	}

	return &models.Resource{
		UserID:          userID,
		ClusterID:       cluster.ID,
		ClusterName:     cluster.Name,
		Name:            namespace.Name,
		Namespace:       "",
		Type:            models.ResourceTypeNamespace,
		UID:             string(namespace.UID),
		ResourceVersion: namespace.ResourceVersion,
		Status:          status,
		Labels:          namespace.Labels,
		Annotations:     namespace.Annotations,
		CreatedAt:       namespace.CreationTimestamp.Time,
	}
}

// GetResources retrieves resources from database with optional filtering
func (s *ResourceService) GetResources(ctx context.Context, userID primitive.ObjectID, filter bson.M) ([]*models.Resource, error) {
	return s.resourceRepo.List(ctx, userID, filter)
}

// GetResourceByID retrieves a single resource by ID
func (s *ResourceService) GetResourceByID(ctx context.Context, id, userID primitive.ObjectID) (*models.Resource, error) {
	return s.resourceRepo.GetByID(ctx, id, userID)
}

// UpdateResourceUserFields updates user-specific fields like tags, notes, favorites
func (s *ResourceService) UpdateResourceUserFields(ctx context.Context, id, userID primitive.ObjectID, updates bson.M) error {
	return s.resourceRepo.UpdateUserFields(ctx, id, userID, updates)
}

// GetResourceHistory retrieves history for a resource
func (s *ResourceService) GetResourceHistory(ctx context.Context, resourceID primitive.ObjectID) ([]*models.ResourceHistory, error) {
	return s.resourceRepo.GetHistory(ctx, resourceID, 100)
}

// RecordResourceAccess records an access event for a resource
func (s *ResourceService) RecordResourceAccess(ctx context.Context, resourceID, userID primitive.ObjectID, action string, details map[string]string) error {
	s.resourceRepo.RecordAccess(ctx, resourceID, userID, action, details)
	return nil
}