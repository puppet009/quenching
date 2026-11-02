package operationevidence

import (
	"encoding/json"
	"time"
)

type LegacyInput struct {
	Type             string  `json:"operation_type"`
	Category         string  `json:"operation_category"`
	Target           string  `json:"target"`
	Detail           any     `json:"detail"`
	Result           string  `json:"result"`
	Penalty          float64 `json:"penalty"`
	VirtualTimestamp int64   `json:"virtual_timestamp_sec"`
}
type LegacyBatch struct {
	RequestID  string        `json:"request_id"`
	SessionID  int64         `json:"session_id"`
	UserID     int64         `json:"user_id"`
	Operations []LegacyInput `json:"operations"`
}
type LegacyReceipt struct {
	RequestID string `json:"request_id"`
	SessionID int64  `json:"session_id"`
	UserID    int64  `json:"user_id"`
	Written   int    `json:"written"`
	Total     int64  `json:"total"`
}
type LegacyRow struct {
	ReplayHash       string          `json:"replay_hash,omitempty"`
	ID               int64           `json:"id"`
	SessionID        int64           `json:"session_id"`
	Type             string          `json:"operation_type"`
	Category         string          `json:"operation_category"`
	Target           string          `json:"target"`
	Detail           json.RawMessage `json:"detail"`
	Result           string          `json:"result"`
	Penalty          float64         `json:"penalty"`
	VirtualTimestamp *int64          `json:"virtual_timestamp"`
	TenantID         int64           `json:"tenant_id"`
	CreatedAt        time.Time       `json:"created_at"`
	DeletedAt        *time.Time      `json:"deleted_at"`
}
type LegacyProjection struct {
	Rows []LegacyRow `json:"rows"`
}

type LegacyCheckpoint struct {
	SessionID int64 `json:"session_id"`
	Watermark int64 `json:"watermark"`
	Pending   int64 `json:"pending"`
}
