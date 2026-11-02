package model

import "time"

type ElasticTarget struct {
	Replicas int32           `json:"replicas"`
	Requests ResourceAmounts `json:"requests"`
}
type ElasticIntent struct {
	ID         string        `json:"id"`
	Target     ElasticTarget `json:"target"`
	Previous   ElasticTarget `json:"previous"`
	Version    string        `json:"resource_version"`
	Generation int64         `json:"generation"`
	// Budget holds max(old,new) during rollout; FinalBudget is reduced only after readiness.
	Budget      *ResourcePlan `json:"budget,omitempty"`
	FinalBudget *ResourcePlan `json:"final_budget,omitempty"`
}
type ElasticPolicy struct {
	Namespace   string          `json:"namespace"`
	Name        string          `json:"name"`
	UID         string          `json:"uid"`
	Version     int64           `json:"version"`
	SessionID   int64           `json:"session_id,omitempty"`
	Component   string          `json:"component,omitempty"`
	Enabled     bool            `json:"enabled"`
	Frozen      bool            `json:"frozen"`
	Min         ResourceAmounts `json:"min"`
	Max         ResourceAmounts `json:"max"`
	Limits      ResourceAmounts `json:"limits"`
	MaxReplicas int32           `json:"max_replicas"`
	Current     ElasticTarget   `json:"current"`
	Pending     *ElasticIntent  `json:"pending,omitempty"`
	LastChanged time.Time       `json:"last_changed"`
	HighSamples int             `json:"-"`
	LowSince    time.Time       `json:"-"`
	LastSample  time.Time       `json:"-"`
	Reason      string          `json:"reason"`
}
type ScalingHold struct {
	Namespace string    `json:"namespace"`
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type ElasticAudit struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}
type ElasticState struct {
	Policies []ElasticPolicy `json:"policies"`
	Holds    []ScalingHold   `json:"holds"`
	Audit    []ElasticAudit  `json:"audit"`
}
