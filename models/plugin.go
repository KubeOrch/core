package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PluginCategory represents the category of a CRD plugin
type PluginCategory string

const (
	PluginCategoryVirtualization PluginCategory = "virtualization"
	PluginCategoryNetworking     PluginCategory = "networking"
	PluginCategoryStorage        PluginCategory = "storage"
	PluginCategoryMonitoring     PluginCategory = "monitoring"
	PluginCategorySecurity       PluginCategory = "security"
	PluginCategoryWorkflow       PluginCategory = "workflow"
	PluginCategoryDatabase       PluginCategory = "database"
	PluginCategoryMessaging      PluginCategory = "messaging"
	PluginCategoryBackup         PluginCategory = "backup"
	PluginCategoryCICD           PluginCategory = "cicd"
	PluginCategoryML             PluginCategory = "ml"
	PluginCategoryPolicy         PluginCategory = "policy"
	PluginCategoryScaling        PluginCategory = "scaling"
)

// PluginStatus represents whether a plugin is enabled or disabled for a user
type PluginStatus string

const (
	PluginStatusEnabled  PluginStatus = "enabled"
	PluginStatusDisabled PluginStatus = "disabled"
)

// Plugin represents a CRD extension plugin definition
type Plugin struct {
	ID          string           `json:"id" bson:"_id"`
	Name        string           `json:"name" bson:"name"`
	DisplayName string           `json:"displayName" bson:"display_name"`
	Description string           `json:"description" bson:"description"`
	Category    PluginCategory   `json:"category" bson:"category"`
	Version     string           `json:"version" bson:"version"`
	CRDGroup    string           `json:"crdGroup" bson:"crd_group"`
	CRDKinds    []string         `json:"crdKinds" bson:"crd_kinds"`
	NodeTypes   []PluginNodeType `json:"nodeTypes" bson:"node_types"`
	CreatedAt   time.Time        `json:"createdAt" bson:"created_at"`
	UpdatedAt   time.Time        `json:"updatedAt" bson:"updated_at"`
}

// PluginNodeType defines a workflow node type provided by a plugin
type PluginNodeType struct {
	Name        string            `json:"name" bson:"name"`
	DisplayName string            `json:"displayName" bson:"display_name"`
	Description string            `json:"description" bson:"description"`
	Category    string            `json:"category" bson:"category"`
	Fields      []PluginNodeField `json:"fields" bson:"fields"`
	DefaultYAML string            `json:"defaultYaml,omitempty" bson:"default_yaml,omitempty"`
}

// PluginNodeField defines a field in the node editor
type PluginNodeField struct {
	ID          string   `json:"id" bson:"id"`
	Label       string   `json:"label" bson:"label"`
	Type        string   `json:"type" bson:"type"` // text, number, select, textarea
	Required    bool     `json:"required" bson:"required"`
	Default     string   `json:"default,omitempty" bson:"default,omitempty"`
	Options     []string `json:"options,omitempty" bson:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty" bson:"placeholder,omitempty"`
}

// UserPlugin tracks which plugins a user has enabled
type UserPlugin struct {
	ID        primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	UserID    primitive.ObjectID `json:"userId" bson:"user_id"`
	PluginID  string             `json:"pluginId" bson:"plugin_id"`
	Status    PluginStatus       `json:"status" bson:"status"`
	EnabledAt time.Time          `json:"enabledAt" bson:"enabled_at"`
	UpdatedAt time.Time          `json:"updatedAt" bson:"updated_at"`
}

// BuiltInPlugins contains the predefined CRD plugins
var BuiltInPlugins = []Plugin{
	// ============================================
	// VIRTUALIZATION
	// ============================================
	{
		ID:          "kubevirt",
		Name:        "kubevirt",
		DisplayName: "KubeVirt",
		Description: "Run virtual machines alongside containers using KubeVirt",
		Category:    PluginCategoryVirtualization,
		Version:     "1.2.0",
		CRDGroup:    "kubevirt.io",
		CRDKinds:    []string{"VirtualMachine", "VirtualMachineInstance", "VirtualMachineInstanceReplicaSet"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "virtualmachine",
				DisplayName: "Virtual Machine",
				Description: "Create a KubeVirt Virtual Machine",
				Category:    "virtualization",
				Fields: []PluginNodeField{
					{ID: "name", Label: "VM Name", Type: "text", Required: true, Placeholder: "my-vm"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "cpu", Label: "CPU Cores", Type: "number", Required: false, Default: "1"},
					{ID: "memory", Label: "Memory", Type: "text", Required: false, Default: "1Gi", Placeholder: "1Gi"},
					{ID: "image", Label: "Container Disk Image", Type: "text", Required: true, Placeholder: "quay.io/kubevirt/cirros-container-disk-demo"},
				},
			},
		},
	},

	// ============================================
	// NETWORKING & SERVICE MESH
	// ============================================
	{
		ID:          "istio",
		Name:        "istio",
		DisplayName: "Istio Service Mesh",
		Description: "Configure Istio service mesh resources for traffic management and security",
		Category:    PluginCategoryNetworking,
		Version:     "1.21.0",
		CRDGroup:    "networking.istio.io",
		CRDKinds:    []string{"VirtualService", "Gateway", "DestinationRule", "ServiceEntry", "Sidecar"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "virtualservice",
				DisplayName: "Virtual Service",
				Description: "Configure traffic routing with Istio Virtual Service",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-virtualservice"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "hosts", Label: "Hosts", Type: "text", Required: true, Placeholder: "my-service"},
					{ID: "gateway", Label: "Gateway", Type: "text", Required: false, Placeholder: "my-gateway"},
				},
			},
			{
				Name:        "istio-gateway",
				DisplayName: "Istio Gateway",
				Description: "Configure an Istio Gateway for ingress traffic",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-gateway"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "istio-system"},
					{ID: "host", Label: "Host", Type: "text", Required: true, Placeholder: "*.example.com"},
					{ID: "port", Label: "Port", Type: "number", Required: false, Default: "80"},
				},
			},
		},
	},
	{
		ID:          "linkerd",
		Name:        "linkerd",
		DisplayName: "Linkerd",
		Description: "Lightweight service mesh for Kubernetes with automatic mTLS",
		Category:    PluginCategoryNetworking,
		Version:     "2.14.0",
		CRDGroup:    "linkerd.io",
		CRDKinds:    []string{"ServiceProfile", "Server", "ServerAuthorization", "HTTPRoute"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "serviceprofile",
				DisplayName: "Service Profile",
				Description: "Define per-route metrics and retries for a service",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-service.default.svc.cluster.local"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
				},
			},
		},
	},
	{
		ID:          "traefik",
		Name:        "traefik",
		DisplayName: "Traefik Proxy",
		Description: "Cloud-native edge router and ingress controller",
		Category:    PluginCategoryNetworking,
		Version:     "3.0.0",
		CRDGroup:    "traefik.io",
		CRDKinds:    []string{"IngressRoute", "IngressRouteTCP", "IngressRouteUDP", "Middleware", "TLSOption"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "ingressroute",
				DisplayName: "Ingress Route",
				Description: "Define custom routing rules with Traefik IngressRoute",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-ingress"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "entryPoints", Label: "Entry Points", Type: "text", Required: true, Default: "web,websecure"},
					{ID: "host", Label: "Host Match", Type: "text", Required: true, Placeholder: "example.com"},
				},
			},
		},
	},

	// ============================================
	// DATABASES
	// ============================================
	{
		ID:          "postgresql",
		Name:        "postgresql",
		DisplayName: "CloudNativePG",
		Description: "Cloud-native PostgreSQL operator for high availability clusters",
		Category:    PluginCategoryDatabase,
		Version:     "1.22.0",
		CRDGroup:    "postgresql.cnpg.io",
		CRDKinds:    []string{"Cluster", "Backup", "ScheduledBackup", "Pooler"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "pg-cluster",
				DisplayName: "PostgreSQL Cluster",
				Description: "Deploy a highly available PostgreSQL cluster",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Cluster Name", Type: "text", Required: true, Placeholder: "my-postgres"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "instances", Label: "Instances", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
					{ID: "version", Label: "PostgreSQL Version", Type: "select", Required: false, Default: "16", Options: []string{"16", "15", "14", "13"}},
				},
			},
		},
	},
	{
		ID:          "mysql",
		Name:        "mysql",
		DisplayName: "MySQL Operator",
		Description: "Oracle MySQL Operator for Kubernetes InnoDB clusters",
		Category:    PluginCategoryDatabase,
		Version:     "8.3.0",
		CRDGroup:    "mysql.oracle.com",
		CRDKinds:    []string{"InnoDBCluster", "MySQLBackup", "ClusterSecretRef"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "innodb-cluster",
				DisplayName: "MySQL InnoDB Cluster",
				Description: "Deploy a MySQL InnoDB cluster with automatic failover",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Cluster Name", Type: "text", Required: true, Placeholder: "my-mysql"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "instances", Label: "Instances", Type: "number", Required: false, Default: "3"},
					{ID: "secretName", Label: "Secret Name", Type: "text", Required: true, Placeholder: "mysql-root-secret"},
				},
			},
		},
	},
	{
		ID:          "mongodb",
		Name:        "mongodb",
		DisplayName: "MongoDB Community",
		Description: "MongoDB Community Kubernetes Operator for replica sets",
		Category:    PluginCategoryDatabase,
		Version:     "0.9.0",
		CRDGroup:    "mongodbcommunity.mongodb.com",
		CRDKinds:    []string{"MongoDBCommunity"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "mongodb-replicaset",
				DisplayName: "MongoDB Replica Set",
				Description: "Deploy a MongoDB replica set cluster",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-mongodb"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "members", Label: "Members", Type: "number", Required: false, Default: "3"},
					{ID: "version", Label: "MongoDB Version", Type: "text", Required: false, Default: "6.0.5"},
				},
			},
		},
	},
	{
		ID:          "redis",
		Name:        "redis",
		DisplayName: "Redis Operator",
		Description: "Manage Redis clusters and sentinels on Kubernetes",
		Category:    PluginCategoryDatabase,
		Version:     "0.17.0",
		CRDGroup:    "redis.redis.opstreelabs.in",
		CRDKinds:    []string{"Redis", "RedisCluster", "RedisSentinel", "RedisReplication"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "redis-cluster",
				DisplayName: "Redis Cluster",
				Description: "Deploy a Redis cluster with automatic sharding",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-redis"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "clusterSize", Label: "Cluster Size", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "1Gi"},
				},
			},
		},
	},
	{
		ID:          "elasticsearch",
		Name:        "elasticsearch",
		DisplayName: "Elastic Cloud on K8s",
		Description: "Official Elasticsearch, Kibana, and APM Server operator",
		Category:    PluginCategoryDatabase,
		Version:     "2.11.0",
		CRDGroup:    "elasticsearch.k8s.elastic.co",
		CRDKinds:    []string{"Elasticsearch", "Kibana", "ApmServer", "Beat", "Agent"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "elasticsearch-cluster",
				DisplayName: "Elasticsearch Cluster",
				Description: "Deploy an Elasticsearch cluster",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-elasticsearch"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "version", Label: "Version", Type: "text", Required: false, Default: "8.11.0"},
					{ID: "nodeCount", Label: "Node Count", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
				},
			},
			{
				Name:        "kibana",
				DisplayName: "Kibana",
				Description: "Deploy Kibana for Elasticsearch visualization",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-kibana"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "version", Label: "Version", Type: "text", Required: false, Default: "8.11.0"},
					{ID: "elasticsearchRef", Label: "Elasticsearch Ref", Type: "text", Required: true, Placeholder: "my-elasticsearch"},
				},
			},
		},
	},
	{
		ID:          "cassandra",
		Name:        "cassandra",
		DisplayName: "K8ssandra",
		Description: "Production-ready Apache Cassandra on Kubernetes",
		Category:    PluginCategoryDatabase,
		Version:     "1.15.0",
		CRDGroup:    "k8ssandra.io",
		CRDKinds:    []string{"K8ssandraCluster", "Stargate", "Reaper"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "cassandra-cluster",
				DisplayName: "Cassandra Cluster",
				Description: "Deploy an Apache Cassandra cluster with K8ssandra",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-cassandra"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "size", Label: "Cluster Size", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
				},
			},
		},
	},

	// ============================================
	// MESSAGING & STREAMING
	// ============================================
	{
		ID:          "kafka",
		Name:        "kafka",
		DisplayName: "Strimzi Kafka",
		Description: "Run Apache Kafka on Kubernetes with Strimzi operator",
		Category:    PluginCategoryMessaging,
		Version:     "0.39.0",
		CRDGroup:    "kafka.strimzi.io",
		CRDKinds:    []string{"Kafka", "KafkaTopic", "KafkaUser", "KafkaConnect", "KafkaBridge", "KafkaMirrorMaker2"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "kafka-cluster",
				DisplayName: "Kafka Cluster",
				Description: "Deploy an Apache Kafka cluster",
				Category:    "messaging",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-kafka"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "replicas", Label: "Broker Replicas", Type: "number", Required: false, Default: "3"},
					{ID: "zookeeperReplicas", Label: "Zookeeper Replicas", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
				},
			},
			{
				Name:        "kafka-topic",
				DisplayName: "Kafka Topic",
				Description: "Create a Kafka topic",
				Category:    "messaging",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Topic Name", Type: "text", Required: true, Placeholder: "my-topic"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "partitions", Label: "Partitions", Type: "number", Required: false, Default: "3"},
					{ID: "replicas", Label: "Replicas", Type: "number", Required: false, Default: "3"},
					{ID: "clusterName", Label: "Kafka Cluster", Type: "text", Required: true, Placeholder: "my-kafka"},
				},
			},
		},
	},
	{
		ID:          "rabbitmq",
		Name:        "rabbitmq",
		DisplayName: "RabbitMQ Cluster",
		Description: "Deploy and manage RabbitMQ clusters on Kubernetes",
		Category:    PluginCategoryMessaging,
		Version:     "2.7.0",
		CRDGroup:    "rabbitmq.com",
		CRDKinds:    []string{"RabbitmqCluster", "Queue", "Exchange", "Binding", "User", "Vhost"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "rabbitmq-cluster",
				DisplayName: "RabbitMQ Cluster",
				Description: "Deploy a RabbitMQ cluster",
				Category:    "messaging",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-rabbitmq"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "replicas", Label: "Replicas", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
				},
			},
		},
	},
	{
		ID:          "nats",
		Name:        "nats",
		DisplayName: "NATS",
		Description: "Cloud-native messaging system for microservices",
		Category:    PluginCategoryMessaging,
		Version:     "0.23.0",
		CRDGroup:    "nats.io",
		CRDKinds:    []string{"NatsCluster", "NatsServiceRole", "NatsAccount"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "nats-cluster",
				DisplayName: "NATS Cluster",
				Description: "Deploy a NATS messaging cluster",
				Category:    "messaging",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-nats"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "size", Label: "Cluster Size", Type: "number", Required: false, Default: "3"},
				},
			},
		},
	},

	// ============================================
	// MONITORING & OBSERVABILITY
	// ============================================
	{
		ID:          "prometheus",
		Name:        "prometheus",
		DisplayName: "Prometheus Operator",
		Description: "Monitor Kubernetes with Prometheus, Alertmanager, and Grafana",
		Category:    PluginCategoryMonitoring,
		Version:     "0.72.0",
		CRDGroup:    "monitoring.coreos.com",
		CRDKinds:    []string{"Prometheus", "Alertmanager", "ServiceMonitor", "PodMonitor", "PrometheusRule"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "prometheus-instance",
				DisplayName: "Prometheus Instance",
				Description: "Deploy a Prometheus monitoring instance",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "prometheus"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "monitoring"},
					{ID: "replicas", Label: "Replicas", Type: "number", Required: false, Default: "2"},
					{ID: "retention", Label: "Retention", Type: "text", Required: false, Default: "15d"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "50Gi"},
				},
			},
			{
				Name:        "servicemonitor",
				DisplayName: "Service Monitor",
				Description: "Define how services should be monitored by Prometheus",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-app-monitor"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "selector", Label: "Service Selector", Type: "text", Required: true, Placeholder: "app=my-app"},
					{ID: "port", Label: "Metrics Port", Type: "text", Required: false, Default: "metrics"},
					{ID: "interval", Label: "Scrape Interval", Type: "text", Required: false, Default: "30s"},
				},
			},
			{
				Name:        "alertmanager-instance",
				DisplayName: "Alertmanager",
				Description: "Deploy Alertmanager for alert handling",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "alertmanager"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "monitoring"},
					{ID: "replicas", Label: "Replicas", Type: "number", Required: false, Default: "3"},
				},
			},
		},
	},
	{
		ID:          "grafana",
		Name:        "grafana",
		DisplayName: "Grafana Operator",
		Description: "Manage Grafana instances, dashboards, and datasources",
		Category:    PluginCategoryMonitoring,
		Version:     "5.6.0",
		CRDGroup:    "grafana.integreatly.org",
		CRDKinds:    []string{"Grafana", "GrafanaDashboard", "GrafanaDatasource", "GrafanaFolder"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "grafana-instance",
				DisplayName: "Grafana Instance",
				Description: "Deploy a Grafana instance",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "grafana"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "monitoring"},
					{ID: "replicas", Label: "Replicas", Type: "number", Required: false, Default: "1"},
				},
			},
			{
				Name:        "grafana-dashboard",
				DisplayName: "Grafana Dashboard",
				Description: "Deploy a Grafana dashboard from JSON",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-dashboard"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "monitoring"},
					{ID: "instanceSelector", Label: "Grafana Instance", Type: "text", Required: true, Placeholder: "grafana"},
				},
			},
		},
	},
	{
		ID:          "jaeger",
		Name:        "jaeger",
		DisplayName: "Jaeger Tracing",
		Description: "Distributed tracing platform for monitoring microservices",
		Category:    PluginCategoryMonitoring,
		Version:     "1.53.0",
		CRDGroup:    "jaegertracing.io",
		CRDKinds:    []string{"Jaeger"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "jaeger-instance",
				DisplayName: "Jaeger Instance",
				Description: "Deploy a Jaeger tracing backend",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "jaeger"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "observability"},
					{ID: "strategy", Label: "Strategy", Type: "select", Required: false, Default: "production", Options: []string{"allInOne", "production", "streaming"}},
				},
			},
		},
	},

	// ============================================
	// STORAGE
	// ============================================
	{
		ID:          "rook-ceph",
		Name:        "rook-ceph",
		DisplayName: "Rook Ceph",
		Description: "Cloud-native storage orchestration for Kubernetes using Ceph",
		Category:    PluginCategoryStorage,
		Version:     "1.13.0",
		CRDGroup:    "ceph.rook.io",
		CRDKinds:    []string{"CephCluster", "CephBlockPool", "CephFilesystem", "CephObjectStore"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "ceph-cluster",
				DisplayName: "Ceph Cluster",
				Description: "Deploy a Ceph storage cluster",
				Category:    "storage",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "rook-ceph"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "rook-ceph"},
					{ID: "useAllNodes", Label: "Use All Nodes", Type: "select", Required: false, Default: "true", Options: []string{"true", "false"}},
					{ID: "useAllDevices", Label: "Use All Devices", Type: "select", Required: false, Default: "true", Options: []string{"true", "false"}},
				},
			},
			{
				Name:        "ceph-blockpool",
				DisplayName: "Ceph Block Pool",
				Description: "Create a Ceph block storage pool",
				Category:    "storage",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "replicapool"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "rook-ceph"},
					{ID: "replicated", Label: "Replica Count", Type: "number", Required: false, Default: "3"},
				},
			},
		},
	},
	{
		ID:          "longhorn",
		Name:        "longhorn",
		DisplayName: "Longhorn",
		Description: "Cloud-native distributed block storage for Kubernetes",
		Category:    PluginCategoryStorage,
		Version:     "1.6.0",
		CRDGroup:    "longhorn.io",
		CRDKinds:    []string{"Volume", "Engine", "Replica", "BackupTarget", "RecurringJob"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "longhorn-volume",
				DisplayName: "Longhorn Volume",
				Description: "Create a Longhorn persistent volume",
				Category:    "storage",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-volume"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "longhorn-system"},
					{ID: "size", Label: "Size", Type: "text", Required: true, Placeholder: "10Gi"},
					{ID: "numberOfReplicas", Label: "Replicas", Type: "number", Required: false, Default: "3"},
				},
			},
		},
	},

	// ============================================
	// SECURITY
	// ============================================
	{
		ID:          "certmanager",
		Name:        "certmanager",
		DisplayName: "Cert-Manager",
		Description: "Automate TLS certificate management with cert-manager",
		Category:    PluginCategorySecurity,
		Version:     "1.14.0",
		CRDGroup:    "cert-manager.io",
		CRDKinds:    []string{"Certificate", "Issuer", "ClusterIssuer", "CertificateRequest"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "certificate",
				DisplayName: "Certificate",
				Description: "Request a TLS certificate from cert-manager",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-cert"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "secretName", Label: "Secret Name", Type: "text", Required: true, Placeholder: "my-cert-tls"},
					{ID: "issuerName", Label: "Issuer Name", Type: "text", Required: true, Placeholder: "letsencrypt-prod"},
					{ID: "dnsNames", Label: "DNS Names", Type: "text", Required: true, Placeholder: "example.com,www.example.com"},
				},
			},
			{
				Name:        "clusterissuer",
				DisplayName: "Cluster Issuer",
				Description: "Create a cluster-wide certificate issuer",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "letsencrypt-prod"},
					{ID: "email", Label: "ACME Email", Type: "text", Required: true, Placeholder: "admin@example.com"},
					{ID: "server", Label: "ACME Server", Type: "select", Required: false, Default: "https://acme-v02.api.letsencrypt.org/directory", Options: []string{"https://acme-v02.api.letsencrypt.org/directory", "https://acme-staging-v02.api.letsencrypt.org/directory"}},
				},
			},
		},
	},
	{
		ID:          "external-secrets",
		Name:        "external-secrets",
		DisplayName: "External Secrets",
		Description: "Sync secrets from external secret stores (AWS, GCP, Vault, etc.)",
		Category:    PluginCategorySecurity,
		Version:     "0.9.0",
		CRDGroup:    "external-secrets.io",
		CRDKinds:    []string{"ExternalSecret", "SecretStore", "ClusterSecretStore"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "externalsecret",
				DisplayName: "External Secret",
				Description: "Sync a secret from an external secret store",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-secret"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "secretStoreRef", Label: "Secret Store", Type: "text", Required: true, Placeholder: "vault-backend"},
					{ID: "remoteKey", Label: "Remote Key", Type: "text", Required: true, Placeholder: "secret/data/my-secret"},
				},
			},
		},
	},
	{
		ID:          "vault",
		Name:        "vault",
		DisplayName: "HashiCorp Vault",
		Description: "Manage secrets and protect sensitive data with Vault",
		Category:    PluginCategorySecurity,
		Version:     "0.5.0",
		CRDGroup:    "vault.hashicorp.com",
		CRDKinds:    []string{"VaultAuth", "VaultConnection", "VaultStaticSecret", "VaultDynamicSecret"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "vault-static-secret",
				DisplayName: "Vault Static Secret",
				Description: "Sync a static secret from HashiCorp Vault",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-vault-secret"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "mount", Label: "Secret Mount", Type: "text", Required: true, Default: "secret"},
					{ID: "path", Label: "Secret Path", Type: "text", Required: true, Placeholder: "my-app/config"},
				},
			},
		},
	},

	// ============================================
	// CI/CD & GITOPS
	// ============================================
	{
		ID:          "argocd",
		Name:        "argocd",
		DisplayName: "Argo CD",
		Description: "Declarative GitOps continuous delivery for Kubernetes",
		Category:    PluginCategoryCICD,
		Version:     "2.10.0",
		CRDGroup:    "argoproj.io",
		CRDKinds:    []string{"Application", "ApplicationSet", "AppProject"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "argocd-application",
				DisplayName: "Argo CD Application",
				Description: "Deploy an application with Argo CD GitOps",
				Category:    "cicd",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-app"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "argocd"},
					{ID: "repoURL", Label: "Git Repository", Type: "text", Required: true, Placeholder: "https://github.com/org/repo"},
					{ID: "path", Label: "Path", Type: "text", Required: true, Placeholder: "k8s/overlays/production"},
					{ID: "targetRevision", Label: "Target Revision", Type: "text", Required: false, Default: "HEAD"},
					{ID: "destNamespace", Label: "Destination Namespace", Type: "text", Required: true, Placeholder: "default"},
				},
			},
		},
	},
	{
		ID:          "argoworkflows",
		Name:        "argoworkflows",
		DisplayName: "Argo Workflows",
		Description: "Container-native workflow engine for orchestrating parallel jobs",
		Category:    PluginCategoryWorkflow,
		Version:     "3.5.0",
		CRDGroup:    "argoproj.io",
		CRDKinds:    []string{"Workflow", "CronWorkflow", "WorkflowTemplate", "ClusterWorkflowTemplate"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "argo-workflow",
				DisplayName: "Argo Workflow",
				Description: "Create an Argo Workflow for complex job orchestration",
				Category:    "workflow",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-workflow"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "argo"},
					{ID: "entrypoint", Label: "Entrypoint", Type: "text", Required: true, Placeholder: "main"},
				},
			},
		},
	},
	{
		ID:          "tekton",
		Name:        "tekton",
		DisplayName: "Tekton Pipelines",
		Description: "Cloud-native CI/CD pipelines for Kubernetes",
		Category:    PluginCategoryCICD,
		Version:     "0.56.0",
		CRDGroup:    "tekton.dev",
		CRDKinds:    []string{"Pipeline", "PipelineRun", "Task", "TaskRun", "Trigger", "TriggerTemplate"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "tekton-pipeline",
				DisplayName: "Tekton Pipeline",
				Description: "Define a CI/CD pipeline with Tekton",
				Category:    "cicd",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "build-and-deploy"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "tekton-pipelines"},
				},
			},
			{
				Name:        "tekton-task",
				DisplayName: "Tekton Task",
				Description: "Define a reusable task for Tekton pipelines",
				Category:    "cicd",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "build-image"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "tekton-pipelines"},
				},
			},
		},
	},
	{
		ID:          "flux",
		Name:        "flux",
		DisplayName: "Flux CD",
		Description: "GitOps toolkit for continuous delivery with Git as source of truth",
		Category:    PluginCategoryCICD,
		Version:     "2.2.0",
		CRDGroup:    "source.toolkit.fluxcd.io",
		CRDKinds:    []string{"GitRepository", "HelmRepository", "Kustomization", "HelmRelease"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "flux-gitrepository",
				DisplayName: "Git Repository",
				Description: "Define a Git repository source for Flux",
				Category:    "cicd",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-repo"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "flux-system"},
					{ID: "url", Label: "Repository URL", Type: "text", Required: true, Placeholder: "https://github.com/org/repo"},
					{ID: "branch", Label: "Branch", Type: "text", Required: false, Default: "main"},
					{ID: "interval", Label: "Sync Interval", Type: "text", Required: false, Default: "1m"},
				},
			},
			{
				Name:        "flux-kustomization",
				DisplayName: "Kustomization",
				Description: "Define a Kustomization for Flux to apply",
				Category:    "cicd",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-app"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "flux-system"},
					{ID: "sourceRef", Label: "Source Reference", Type: "text", Required: true, Placeholder: "my-repo"},
					{ID: "path", Label: "Path", Type: "text", Required: true, Placeholder: "./deploy/production"},
				},
			},
		},
	},

	// ============================================
	// BACKUP & DISASTER RECOVERY
	// ============================================
	{
		ID:          "velero",
		Name:        "velero",
		DisplayName: "Velero",
		Description: "Backup and restore Kubernetes resources and persistent volumes",
		Category:    PluginCategoryBackup,
		Version:     "1.13.0",
		CRDGroup:    "velero.io",
		CRDKinds:    []string{"Backup", "Restore", "Schedule", "BackupStorageLocation", "VolumeSnapshotLocation"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "velero-backup",
				DisplayName: "Velero Backup",
				Description: "Create a backup of Kubernetes resources",
				Category:    "backup",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-backup"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "velero"},
					{ID: "includedNamespaces", Label: "Include Namespaces", Type: "text", Required: false, Placeholder: "default,production"},
					{ID: "ttl", Label: "TTL", Type: "text", Required: false, Default: "720h"},
				},
			},
			{
				Name:        "velero-schedule",
				DisplayName: "Backup Schedule",
				Description: "Schedule recurring backups with Velero",
				Category:    "backup",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "daily-backup"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "velero"},
					{ID: "schedule", Label: "Cron Schedule", Type: "text", Required: true, Default: "0 2 * * *"},
					{ID: "includedNamespaces", Label: "Include Namespaces", Type: "text", Required: false, Placeholder: "default,production"},
				},
			},
		},
	},

	// ============================================
	// POLICY & GOVERNANCE
	// ============================================
	{
		ID:          "kyverno",
		Name:        "kyverno",
		DisplayName: "Kyverno",
		Description: "Kubernetes-native policy management for validation and mutation",
		Category:    PluginCategoryPolicy,
		Version:     "1.11.0",
		CRDGroup:    "kyverno.io",
		CRDKinds:    []string{"ClusterPolicy", "Policy", "PolicyException", "ClusterCleanupPolicy"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "kyverno-policy",
				DisplayName: "Kyverno Policy",
				Description: "Define a Kyverno policy for validation or mutation",
				Category:    "policy",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "require-labels"},
					{ID: "validationFailureAction", Label: "Failure Action", Type: "select", Required: false, Default: "Enforce", Options: []string{"Enforce", "Audit"}},
				},
			},
		},
	},
	{
		ID:          "gatekeeper",
		Name:        "gatekeeper",
		DisplayName: "OPA Gatekeeper",
		Description: "Policy controller for Kubernetes using Open Policy Agent",
		Category:    PluginCategoryPolicy,
		Version:     "3.15.0",
		CRDGroup:    "constraints.gatekeeper.sh",
		CRDKinds:    []string{"ConstraintTemplate", "Config", "Assign", "AssignMetadata"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "constraint-template",
				DisplayName: "Constraint Template",
				Description: "Define a reusable constraint template with Rego",
				Category:    "policy",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "k8srequiredlabels"},
				},
			},
		},
	},

	// ============================================
	// SCALING
	// ============================================
	{
		ID:          "keda",
		Name:        "keda",
		DisplayName: "KEDA",
		Description: "Kubernetes Event-driven Autoscaling for scaling based on events",
		Category:    PluginCategoryScaling,
		Version:     "2.13.0",
		CRDGroup:    "keda.sh",
		CRDKinds:    []string{"ScaledObject", "ScaledJob", "TriggerAuthentication", "ClusterTriggerAuthentication"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "scaled-object",
				DisplayName: "Scaled Object",
				Description: "Configure event-driven autoscaling with KEDA",
				Category:    "scaling",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-scaledobject"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "scaleTargetRef", Label: "Scale Target", Type: "text", Required: true, Placeholder: "my-deployment"},
					{ID: "minReplicaCount", Label: "Min Replicas", Type: "number", Required: false, Default: "0"},
					{ID: "maxReplicaCount", Label: "Max Replicas", Type: "number", Required: false, Default: "10"},
				},
			},
		},
	},

	// ============================================
	// ML & DATA PROCESSING
	// ============================================
	{
		ID:          "kubeflow",
		Name:        "kubeflow",
		DisplayName: "Kubeflow",
		Description: "Machine learning toolkit for Kubernetes",
		Category:    PluginCategoryML,
		Version:     "1.8.0",
		CRDGroup:    "kubeflow.org",
		CRDKinds:    []string{"Notebook", "TFJob", "PyTorchJob", "MPIJob", "Experiment"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "kubeflow-notebook",
				DisplayName: "Jupyter Notebook",
				Description: "Deploy a Kubeflow Jupyter notebook server",
				Category:    "ml",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-notebook"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "kubeflow"},
					{ID: "image", Label: "Image", Type: "text", Required: true, Default: "kubeflownotebookswg/jupyter-scipy:v1.7.0"},
					{ID: "cpu", Label: "CPU", Type: "text", Required: false, Default: "1"},
					{ID: "memory", Label: "Memory", Type: "text", Required: false, Default: "2Gi"},
				},
			},
			{
				Name:        "pytorch-job",
				DisplayName: "PyTorch Job",
				Description: "Run a distributed PyTorch training job",
				Category:    "ml",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "pytorch-training"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "kubeflow"},
					{ID: "image", Label: "Image", Type: "text", Required: true, Placeholder: "pytorch/pytorch:latest"},
					{ID: "replicas", Label: "Worker Replicas", Type: "number", Required: false, Default: "2"},
				},
			},
		},
	},
	{
		ID:          "spark",
		Name:        "spark",
		DisplayName: "Spark Operator",
		Description: "Run Apache Spark applications on Kubernetes",
		Category:    PluginCategoryML,
		Version:     "1.1.0",
		CRDGroup:    "sparkoperator.k8s.io",
		CRDKinds:    []string{"SparkApplication", "ScheduledSparkApplication"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "spark-application",
				DisplayName: "Spark Application",
				Description: "Run an Apache Spark application",
				Category:    "ml",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "spark-pi"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "type", Label: "Type", Type: "select", Required: true, Default: "Python", Options: []string{"Scala", "Java", "Python", "R"}},
					{ID: "mainApplicationFile", Label: "Main File", Type: "text", Required: true, Placeholder: "local:///opt/spark/examples/src/main/python/pi.py"},
					{ID: "sparkVersion", Label: "Spark Version", Type: "text", Required: false, Default: "3.5.0"},
				},
			},
		},
	},

	// ============================================
	// INFRASTRUCTURE AS CODE
	// ============================================
	{
		ID:          "crossplane",
		Name:        "crossplane",
		DisplayName: "Crossplane",
		Description: "Manage cloud infrastructure using Kubernetes APIs",
		Category:    PluginCategoryStorage,
		Version:     "1.15.0",
		CRDGroup:    "crossplane.io",
		CRDKinds:    []string{"Provider", "ProviderConfig", "CompositeResourceDefinition", "Composition"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "crossplane-provider",
				DisplayName: "Crossplane Provider",
				Description: "Install a Crossplane provider for cloud resources",
				Category:    "storage",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "provider-aws"},
					{ID: "package", Label: "Package", Type: "text", Required: true, Placeholder: "xpkg.upbound.io/upbound/provider-aws:v0.47.0"},
				},
			},
		},
	},

	// ============================================
	// CNCF GRADUATED - NETWORKING
	// ============================================
	{
		ID:          "cilium",
		Name:        "cilium",
		DisplayName: "Cilium",
		Description: "eBPF-based networking, security, and observability for cloud-native environments",
		Category:    PluginCategoryNetworking,
		Version:     "1.15.0",
		CRDGroup:    "cilium.io",
		CRDKinds:    []string{"CiliumNetworkPolicy", "CiliumClusterwideNetworkPolicy", "CiliumEndpoint", "CiliumIdentity", "CiliumNode"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "cilium-network-policy",
				DisplayName: "Cilium Network Policy",
				Description: "Define L3-L7 network policies with Cilium",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "allow-frontend"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "endpointSelector", Label: "Endpoint Selector", Type: "text", Required: true, Placeholder: "app=frontend"},
				},
			},
		},
	},
	{
		ID:          "envoy-gateway",
		Name:        "envoy-gateway",
		DisplayName: "Envoy Gateway",
		Description: "Kubernetes-native API gateway using Envoy proxy",
		Category:    PluginCategoryNetworking,
		Version:     "1.0.0",
		CRDGroup:    "gateway.envoyproxy.io",
		CRDKinds:    []string{"EnvoyProxy", "BackendTrafficPolicy", "ClientTrafficPolicy", "SecurityPolicy"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "envoy-proxy",
				DisplayName: "Envoy Proxy",
				Description: "Configure Envoy Gateway proxy settings",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-envoy-proxy"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "envoy-gateway-system"},
				},
			},
		},
	},
	{
		ID:          "contour",
		Name:        "contour",
		DisplayName: "Contour",
		Description: "High-performance ingress controller for Kubernetes using Envoy",
		Category:    PluginCategoryNetworking,
		Version:     "1.28.0",
		CRDGroup:    "projectcontour.io",
		CRDKinds:    []string{"HTTPProxy", "TLSCertificateDelegation", "ExtensionService"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "httpproxy",
				DisplayName: "HTTP Proxy",
				Description: "Define advanced HTTP routing with Contour HTTPProxy",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-app-proxy"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "fqdn", Label: "FQDN", Type: "text", Required: true, Placeholder: "app.example.com"},
					{ID: "serviceName", Label: "Service Name", Type: "text", Required: true, Placeholder: "my-app"},
					{ID: "servicePort", Label: "Service Port", Type: "number", Required: false, Default: "80"},
				},
			},
		},
	},
	{
		ID:          "kong",
		Name:        "kong",
		DisplayName: "Kong Ingress",
		Description: "Cloud-native API gateway with plugins for authentication, rate limiting, and more",
		Category:    PluginCategoryNetworking,
		Version:     "3.5.0",
		CRDGroup:    "configuration.konghq.com",
		CRDKinds:    []string{"KongIngress", "KongPlugin", "KongClusterPlugin", "KongConsumer", "TCPIngress", "UDPIngress"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "kong-plugin",
				DisplayName: "Kong Plugin",
				Description: "Configure a Kong plugin for API gateway features",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "rate-limiting"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "plugin", Label: "Plugin Name", Type: "select", Required: true, Default: "rate-limiting", Options: []string{"rate-limiting", "key-auth", "jwt", "cors", "request-transformer", "response-transformer"}},
				},
			},
		},
	},

	// ============================================
	// CNCF GRADUATED - SECURITY
	// ============================================
	{
		ID:          "falco",
		Name:        "falco",
		DisplayName: "Falco",
		Description: "Cloud-native runtime security for threat detection",
		Category:    PluginCategorySecurity,
		Version:     "0.37.0",
		CRDGroup:    "falco.org",
		CRDKinds:    []string{"FalcoRule"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "falco-rule",
				DisplayName: "Falco Rule",
				Description: "Define a custom Falco security rule",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "detect-shell"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "falco"},
				},
			},
		},
	},

	// ============================================
	// CNCF GRADUATED - REGISTRY
	// ============================================
	{
		ID:          "harbor",
		Name:        "harbor",
		DisplayName: "Harbor",
		Description: "Cloud-native container registry with security and vulnerability scanning",
		Category:    PluginCategorySecurity,
		Version:     "2.10.0",
		CRDGroup:    "goharbor.io",
		CRDKinds:    []string{"HarborCluster", "Harbor", "HarborConfiguration"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "harbor-cluster",
				DisplayName: "Harbor Cluster",
				Description: "Deploy a Harbor container registry cluster",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "harbor"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "harbor"},
					{ID: "externalURL", Label: "External URL", Type: "text", Required: true, Placeholder: "https://registry.example.com"},
				},
			},
		},
	},

	// ============================================
	// CNCF GRADUATED - OBSERVABILITY
	// ============================================
	{
		ID:          "fluentbit",
		Name:        "fluentbit",
		DisplayName: "Fluent Bit",
		Description: "Fast and lightweight log processor and forwarder",
		Category:    PluginCategoryMonitoring,
		Version:     "2.2.0",
		CRDGroup:    "fluentbit.fluent.io",
		CRDKinds:    []string{"FluentBit", "FluentBitConfig", "ClusterFilter", "ClusterInput", "ClusterOutput", "ClusterParser"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "fluentbit-config",
				DisplayName: "Fluent Bit Config",
				Description: "Configure Fluent Bit log collection and forwarding",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "fluent-bit"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "logging"},
				},
			},
			{
				Name:        "fluentbit-output",
				DisplayName: "Fluent Bit Output",
				Description: "Define where Fluent Bit sends logs",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "es-output"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "logging"},
					{ID: "outputType", Label: "Output Type", Type: "select", Required: true, Default: "elasticsearch", Options: []string{"elasticsearch", "loki", "cloudwatch", "s3", "kafka"}},
					{ID: "host", Label: "Host", Type: "text", Required: true, Placeholder: "elasticsearch.logging.svc"},
					{ID: "port", Label: "Port", Type: "number", Required: false, Default: "9200"},
				},
			},
		},
	},
	{
		ID:          "opentelemetry",
		Name:        "opentelemetry",
		DisplayName: "OpenTelemetry",
		Description: "Vendor-neutral observability framework for traces, metrics, and logs",
		Category:    PluginCategoryMonitoring,
		Version:     "0.93.0",
		CRDGroup:    "opentelemetry.io",
		CRDKinds:    []string{"OpenTelemetryCollector", "Instrumentation", "OpAMPBridge"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "otel-collector",
				DisplayName: "OTel Collector",
				Description: "Deploy an OpenTelemetry Collector for telemetry data",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "otel-collector"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "observability"},
					{ID: "mode", Label: "Mode", Type: "select", Required: false, Default: "deployment", Options: []string{"deployment", "daemonset", "statefulset", "sidecar"}},
				},
			},
			{
				Name:        "otel-instrumentation",
				DisplayName: "OTel Instrumentation",
				Description: "Auto-instrument applications with OpenTelemetry",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-instrumentation"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "language", Label: "Language", Type: "select", Required: true, Default: "java", Options: []string{"java", "nodejs", "python", "dotnet", "go"}},
				},
			},
		},
	},
	{
		ID:          "loki",
		Name:        "loki",
		DisplayName: "Grafana Loki",
		Description: "Horizontally scalable log aggregation system inspired by Prometheus",
		Category:    PluginCategoryMonitoring,
		Version:     "2.9.0",
		CRDGroup:    "loki.grafana.com",
		CRDKinds:    []string{"LokiStack", "AlertingRule", "RecordingRule", "RulerConfig"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "loki-stack",
				DisplayName: "Loki Stack",
				Description: "Deploy Grafana Loki for log aggregation",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "loki"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "logging"},
					{ID: "size", Label: "Size", Type: "select", Required: false, Default: "1x.small", Options: []string{"1x.extra-small", "1x.small", "1x.medium"}},
					{ID: "storageClass", Label: "Storage Class", Type: "text", Required: false, Default: "standard"},
				},
			},
		},
	},

	// ============================================
	// SERVERLESS
	// ============================================
	{
		ID:          "knative",
		Name:        "knative",
		DisplayName: "Knative",
		Description: "Kubernetes-based platform for serverless workloads",
		Category:    PluginCategoryScaling,
		Version:     "1.13.0",
		CRDGroup:    "serving.knative.dev",
		CRDKinds:    []string{"Service", "Configuration", "Revision", "Route"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "knative-service",
				DisplayName: "Knative Service",
				Description: "Deploy a serverless application with Knative",
				Category:    "scaling",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-serverless-app"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "image", Label: "Container Image", Type: "text", Required: true, Placeholder: "gcr.io/my-project/my-app"},
					{ID: "minScale", Label: "Min Scale", Type: "number", Required: false, Default: "0"},
					{ID: "maxScale", Label: "Max Scale", Type: "number", Required: false, Default: "10"},
				},
			},
		},
	},
	{
		ID:          "dapr",
		Name:        "dapr",
		DisplayName: "Dapr",
		Description: "Distributed application runtime for building microservices",
		Category:    PluginCategoryWorkflow,
		Version:     "1.13.0",
		CRDGroup:    "dapr.io",
		CRDKinds:    []string{"Component", "Configuration", "Subscription", "Resiliency"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "dapr-component",
				DisplayName: "Dapr Component",
				Description: "Define a Dapr building block component (state, pubsub, binding)",
				Category:    "workflow",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "statestore"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "componentType", Label: "Component Type", Type: "select", Required: true, Default: "state.redis", Options: []string{"state.redis", "state.postgresql", "pubsub.redis", "pubsub.kafka", "bindings.kafka", "bindings.rabbitmq"}},
				},
			},
		},
	},

	// ============================================
	// MORE DATABASES
	// ============================================
	{
		ID:          "vitess",
		Name:        "vitess",
		DisplayName: "Vitess",
		Description: "Database clustering system for horizontal scaling of MySQL",
		Category:    PluginCategoryDatabase,
		Version:     "18.0.0",
		CRDGroup:    "planetscale.com",
		CRDKinds:    []string{"VitessCluster", "VitessKeyspace", "VitessShard", "VitessCell"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "vitess-cluster",
				DisplayName: "Vitess Cluster",
				Description: "Deploy a horizontally scalable MySQL cluster with Vitess",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-vitess"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "cells", Label: "Cells", Type: "number", Required: false, Default: "1"},
				},
			},
		},
	},
	{
		ID:          "cockroachdb",
		Name:        "cockroachdb",
		DisplayName: "CockroachDB",
		Description: "Distributed SQL database for global, cloud-native applications",
		Category:    PluginCategoryDatabase,
		Version:     "23.2.0",
		CRDGroup:    "crdb.cockroachlabs.com",
		CRDKinds:    []string{"CrdbCluster"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "crdb-cluster",
				DisplayName: "CockroachDB Cluster",
				Description: "Deploy a CockroachDB distributed SQL cluster",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "cockroachdb"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "nodes", Label: "Nodes", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
				},
			},
		},
	},
	{
		ID:          "mariadb",
		Name:        "mariadb",
		DisplayName: "MariaDB Operator",
		Description: "Deploy and manage MariaDB and Galera clusters on Kubernetes",
		Category:    PluginCategoryDatabase,
		Version:     "0.27.0",
		CRDGroup:    "k8s.mariadb.com",
		CRDKinds:    []string{"MariaDB", "Backup", "Restore", "Connection", "MaxScale"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "mariadb-cluster",
				DisplayName: "MariaDB Cluster",
				Description: "Deploy a MariaDB Galera cluster",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "mariadb"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "replicas", Label: "Replicas", Type: "number", Required: false, Default: "3"},
					{ID: "storage", Label: "Storage Size", Type: "text", Required: false, Default: "10Gi"},
				},
			},
		},
	},
	{
		ID:          "percona-postgresql",
		Name:        "percona-postgresql",
		DisplayName: "Percona PostgreSQL",
		Description: "Enterprise-grade PostgreSQL clustering with Percona",
		Category:    PluginCategoryDatabase,
		Version:     "2.3.0",
		CRDGroup:    "pgv2.percona.com",
		CRDKinds:    []string{"PerconaPGCluster", "PerconaPGBackup", "PerconaPGRestore"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "percona-pg-cluster",
				DisplayName: "Percona PG Cluster",
				Description: "Deploy a Percona PostgreSQL cluster",
				Category:    "database",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "percona-postgres"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "instances", Label: "Instances", Type: "number", Required: false, Default: "3"},
				},
			},
		},
	},

	// ============================================
	// OBJECT STORAGE
	// ============================================
	{
		ID:          "minio",
		Name:        "minio",
		DisplayName: "MinIO",
		Description: "High-performance, S3-compatible object storage",
		Category:    PluginCategoryStorage,
		Version:     "5.0.0",
		CRDGroup:    "minio.min.io",
		CRDKinds:    []string{"Tenant"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "minio-tenant",
				DisplayName: "MinIO Tenant",
				Description: "Deploy a MinIO object storage tenant",
				Category:    "storage",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "minio"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "minio-tenant"},
					{ID: "servers", Label: "Servers", Type: "number", Required: false, Default: "4"},
					{ID: "volumesPerServer", Label: "Volumes Per Server", Type: "number", Required: false, Default: "4"},
					{ID: "storage", Label: "Storage Per Volume", Type: "text", Required: false, Default: "10Gi"},
				},
			},
		},
	},

	// ============================================
	// ML & AI - MODEL SERVING
	// ============================================
	{
		ID:          "kserve",
		Name:        "kserve",
		DisplayName: "KServe",
		Description: "Serverless ML model serving on Kubernetes",
		Category:    PluginCategoryML,
		Version:     "0.12.0",
		CRDGroup:    "serving.kserve.io",
		CRDKinds:    []string{"InferenceService", "TrainedModel", "InferenceGraph", "ClusterServingRuntime"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "inference-service",
				DisplayName: "Inference Service",
				Description: "Deploy an ML model serving endpoint",
				Category:    "ml",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-model"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "framework", Label: "Framework", Type: "select", Required: true, Default: "sklearn", Options: []string{"sklearn", "tensorflow", "pytorch", "xgboost", "onnx", "triton"}},
					{ID: "storageUri", Label: "Model Storage URI", Type: "text", Required: true, Placeholder: "gs://my-bucket/models/my-model"},
				},
			},
		},
	},
	{
		ID:          "ray",
		Name:        "ray",
		DisplayName: "KubeRay",
		Description: "Run Ray distributed computing workloads on Kubernetes",
		Category:    PluginCategoryML,
		Version:     "1.0.0",
		CRDGroup:    "ray.io",
		CRDKinds:    []string{"RayCluster", "RayJob", "RayService"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "ray-cluster",
				DisplayName: "Ray Cluster",
				Description: "Deploy a Ray cluster for distributed computing",
				Category:    "ml",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "ray-cluster"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "headCpu", Label: "Head Node CPU", Type: "text", Required: false, Default: "2"},
					{ID: "headMemory", Label: "Head Node Memory", Type: "text", Required: false, Default: "4Gi"},
					{ID: "workerReplicas", Label: "Worker Replicas", Type: "number", Required: false, Default: "2"},
				},
			},
			{
				Name:        "ray-job",
				DisplayName: "Ray Job",
				Description: "Submit a Ray job to a cluster",
				Category:    "ml",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-ray-job"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "entrypoint", Label: "Entrypoint", Type: "text", Required: true, Placeholder: "python my_script.py"},
				},
			},
		},
	},

	// ============================================
	// SERVICE MESH - ADDITIONAL
	// ============================================
	{
		ID:          "consul",
		Name:        "consul",
		DisplayName: "HashiCorp Consul",
		Description: "Service mesh and service discovery for Kubernetes",
		Category:    PluginCategoryNetworking,
		Version:     "1.3.0",
		CRDGroup:    "consul.hashicorp.com",
		CRDKinds:    []string{"ServiceDefaults", "ServiceResolver", "ServiceRouter", "ServiceSplitter", "IngressGateway", "TerminatingGateway"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "consul-service-defaults",
				DisplayName: "Service Defaults",
				Description: "Configure default settings for a Consul service",
				Category:    "networking",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Service Name", Type: "text", Required: true, Placeholder: "my-service"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "protocol", Label: "Protocol", Type: "select", Required: false, Default: "http", Options: []string{"http", "http2", "grpc", "tcp"}},
				},
			},
		},
	},

	// ============================================
	// SECRETS MANAGEMENT - ADDITIONAL
	// ============================================
	{
		ID:          "sealed-secrets",
		Name:        "sealed-secrets",
		DisplayName: "Sealed Secrets",
		Description: "Encrypt secrets for safe storage in Git repositories",
		Category:    PluginCategorySecurity,
		Version:     "0.25.0",
		CRDGroup:    "bitnami.com",
		CRDKinds:    []string{"SealedSecret"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "sealed-secret",
				DisplayName: "Sealed Secret",
				Description: "Create an encrypted secret for GitOps workflows",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-sealed-secret"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
				},
			},
		},
	},

	// ============================================
	// CHAOS ENGINEERING
	// ============================================
	{
		ID:          "chaos-mesh",
		Name:        "chaos-mesh",
		DisplayName: "Chaos Mesh",
		Description: "Cloud-native chaos engineering platform for Kubernetes",
		Category:    PluginCategorySecurity,
		Version:     "2.6.0",
		CRDGroup:    "chaos-mesh.org",
		CRDKinds:    []string{"PodChaos", "NetworkChaos", "IOChaos", "StressChaos", "TimeChaos", "Workflow"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "pod-chaos",
				DisplayName: "Pod Chaos",
				Description: "Inject pod-level chaos (kill, failure, container kill)",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "pod-failure"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "action", Label: "Action", Type: "select", Required: true, Default: "pod-kill", Options: []string{"pod-kill", "pod-failure", "container-kill"}},
					{ID: "selector", Label: "Pod Selector", Type: "text", Required: true, Placeholder: "app=my-app"},
				},
			},
			{
				Name:        "network-chaos",
				DisplayName: "Network Chaos",
				Description: "Inject network chaos (latency, loss, partition)",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "network-delay"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "action", Label: "Action", Type: "select", Required: true, Default: "delay", Options: []string{"delay", "loss", "duplicate", "corrupt", "partition", "bandwidth"}},
					{ID: "selector", Label: "Pod Selector", Type: "text", Required: true, Placeholder: "app=my-app"},
				},
			},
		},
	},
	{
		ID:          "litmus",
		Name:        "litmus",
		DisplayName: "LitmusChaos",
		Description: "Open-source chaos engineering framework for Kubernetes",
		Category:    PluginCategorySecurity,
		Version:     "3.6.0",
		CRDGroup:    "litmuschaos.io",
		CRDKinds:    []string{"ChaosEngine", "ChaosExperiment", "ChaosResult"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "chaos-engine",
				DisplayName: "Chaos Engine",
				Description: "Run chaos experiments with Litmus",
				Category:    "security",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-chaos"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "appLabel", Label: "Application Label", Type: "text", Required: true, Placeholder: "app=my-app"},
					{ID: "experiment", Label: "Experiment", Type: "text", Required: true, Placeholder: "pod-delete"},
				},
			},
		},
	},

	// ============================================
	// COST MANAGEMENT
	// ============================================
	{
		ID:          "kubecost",
		Name:        "kubecost",
		DisplayName: "Kubecost",
		Description: "Real-time cost monitoring and management for Kubernetes",
		Category:    PluginCategoryMonitoring,
		Version:     "2.1.0",
		CRDGroup:    "kubecost.com",
		CRDKinds:    []string{"CostAnalyzer"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "cost-analyzer",
				DisplayName: "Cost Analyzer",
				Description: "Deploy Kubecost for cost monitoring",
				Category:    "monitoring",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "kubecost"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "kubecost"},
				},
			},
		},
	},

	// ============================================
	// SERVERLESS FUNCTIONS
	// ============================================
	{
		ID:          "openfaas",
		Name:        "openfaas",
		DisplayName: "OpenFaaS",
		Description: "Serverless functions made simple for Kubernetes",
		Category:    PluginCategoryScaling,
		Version:     "0.27.0",
		CRDGroup:    "openfaas.com",
		CRDKinds:    []string{"Function", "Profile"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "openfaas-function",
				DisplayName: "OpenFaaS Function",
				Description: "Deploy a serverless function with OpenFaaS",
				Category:    "scaling",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-function"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "openfaas-fn"},
					{ID: "image", Label: "Function Image", Type: "text", Required: true, Placeholder: "ghcr.io/openfaas/my-function:latest"},
					{ID: "handler", Label: "Handler", Type: "text", Required: false, Default: "handler.handle"},
				},
			},
		},
	},
	{
		ID:          "fission",
		Name:        "fission",
		DisplayName: "Fission",
		Description: "Fast serverless functions for Kubernetes",
		Category:    PluginCategoryScaling,
		Version:     "1.20.0",
		CRDGroup:    "fission.io",
		CRDKinds:    []string{"Function", "Environment", "Package", "HTTPTrigger", "MessageQueueTrigger", "TimeTrigger"},
		NodeTypes: []PluginNodeType{
			{
				Name:        "fission-function",
				DisplayName: "Fission Function",
				Description: "Deploy a function with Fission",
				Category:    "scaling",
				Fields: []PluginNodeField{
					{ID: "name", Label: "Name", Type: "text", Required: true, Placeholder: "my-function"},
					{ID: "namespace", Label: "Namespace", Type: "text", Required: false, Default: "default"},
					{ID: "environment", Label: "Environment", Type: "select", Required: true, Default: "python", Options: []string{"python", "nodejs", "go", "java", "dotnet"}},
				},
			},
		},
	},
}
