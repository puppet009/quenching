package model

import "time"

type ResourceUsage struct {
	CPU       ResourceAmounts `json:"resources"`
	Timestamp string          `json:"timestamp"`
}
type ResourceNode struct {
	Name          string          `json:"name"`
	UID           string          `json:"uid"`
	Version       string          `json:"resource_version"`
	Ready         bool            `json:"ready"`
	Unschedulable bool            `json:"unschedulable"`
	Eligible      bool            `json:"eligible"`
	Reason        string          `json:"reason,omitempty"`
	Capacity      ResourceAmounts `json:"capacity"`
	Allocatable   ResourceAmounts `json:"allocatable"`
	Requested     ResourceAmounts `json:"requested"`
	Limits        ResourceAmounts `json:"limits"`
	Usage         *ResourceUsage  `json:"usage"`
	PodCount      int             `json:"pod_count"`
}
type ResourceGroup struct {
	Requested    ResourceAmounts `json:"requested"`
	Limits       ResourceAmounts `json:"limits"`
	PodCount     int             `json:"pod_count"`
	StorageBytes int64           `json:"storage_bytes"`
}
type ResourceContainer struct {
	Name     string          `json:"name"`
	Requests ResourceAmounts `json:"requests"`
	Limits   ResourceAmounts `json:"limits"`
}
type ResourceWorkload struct {
	Namespace  string              `json:"namespace"`
	Name       string              `json:"name"`
	Kind       string              `json:"kind"`
	Version    string              `json:"resource_version"`
	Replicas   int32               `json:"replicas"`
	Group      string              `json:"group"`
	Containers []ResourceContainer `json:"containers"`
}
type ResourcePod struct {
	WorkloadUID    string          `json:"-"`
	ContainerNames []string        `json:"-"`
	Namespace      string          `json:"namespace"`
	Name           string          `json:"name"`
	Node           string          `json:"node"`
	Phase          string          `json:"phase"`
	Group          string          `json:"group"`
	Requests       ResourceAmounts `json:"requests"`
	Limits         ResourceAmounts `json:"limits"`
	Usage          *ResourceUsage  `json:"usage"`
}
type ResourcePVC struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	StorageClass string `json:"storage_class"`
	Phase        string `json:"phase"`
	Bytes        int64  `json:"bytes"`
}

// ResourceAllocationUnit is a read-only view of one allocation. Requests and
// limits describe observed Pods; Reserved is only the budget not yet in Pods.
// ResourcePlan is separate so queued plans cannot look like occupied CPU.
type ResourceAllocationUnit struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind"`
	Namespace         string          `json:"namespace,omitempty"`
	SessionID         int64           `json:"session_id,omitempty"`
	UserID            int64           `json:"user_id,omitempty"`
	ProjectID         int64           `json:"project_id,omitempty"`
	TemplateSlug      string          `json:"template_slug,omitempty"`
	Status            string          `json:"status,omitempty"`
	ResourcesAdmitted bool            `json:"resources_admitted,omitempty"`
	AllocationReason  string          `json:"allocation_reason,omitempty"`
	CreatedAt         *time.Time      `json:"created_at,omitempty"`
	ResourcePlan      *ResourcePlan   `json:"resource_plan,omitempty"`
	Requested         ResourceAmounts `json:"requested"`
	Limits            ResourceAmounts `json:"limits"`
	Reserved          ResourceAmounts `json:"reserved"`
	PodCount          int             `json:"pod_count"`
	StorageBytes      int64           `json:"storage_bytes"`
	Usage             *ResourceUsage  `json:"usage"`
	UsageSampledPods  int             `json:"usage_sampled_pods"`
}

type PoolSnapshot struct {
	Elastic           *ElasticState              `json:"elastic,omitempty"`
	ObservedAt        time.Time                  `json:"observed_at"`
	Nodes             []ResourceNode             `json:"nodes"`
	Pods              []ResourcePod              `json:"pods"`
	Workloads         []ResourceWorkload         `json:"workloads"`
	PVCs              []ResourcePVC              `json:"pvcs"`
	Groups            map[string]ResourceGroup   `json:"groups"`
	Available         ResourceAmounts            `json:"available"`
	Reserved          ResourceAmounts            `json:"reserved"`
	Pending           ResourceAmounts            `json:"pending"`
	NamespaceRequests map[string]ResourceAmounts `json:"namespace_requests"`
	MetricsAvailable  bool                       `json:"metrics_available"`
	Warnings          []string                   `json:"warnings"`
	Allocations       []Sandbox                  `json:"allocations"`
	AllocationUnits   []ResourceAllocationUnit   `json:"allocation_units"`
	// NamespaceReserved attributes existing budget deductions for the view.
	NamespaceReserved map[string]ResourceAmounts `json:"-"`
}
