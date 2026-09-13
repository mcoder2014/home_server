package siteconfig

import "time"

type PublishRequest struct {
	RequestID string                 `json:"request_id"`
	Reason    string                 `json:"reason"`
	Values    map[string]interface{} `json:"values"`
}

type RollbackRequest struct {
	TargetRevision int64  `json:"target_revision"`
	RequestID      string `json:"request_id"`
	Reason         string `json:"reason"`
}

type NamespaceView struct {
	Namespace         string                 `json:"namespace"`
	Revision          int64                  `json:"revision"`
	SchemaVersion     int                    `json:"schema_version"`
	Values            map[string]interface{} `json:"values"`
	UpdatedBy         string                 `json:"updated_by"`
	UpdateTime        time.Time              `json:"update_time"`
	PersistedRevision int64                  `json:"persisted_revision"`
	LoadedRevision    int64                  `json:"loaded_revision"`
	ApplyState        string                 `json:"apply_state"`
}

type NamespaceList struct {
	Items []NamespaceView `json:"items"`
}

type HistoryView struct {
	Namespace            string                 `json:"namespace"`
	Revision             int64                  `json:"revision"`
	Values               map[string]interface{} `json:"values"`
	ActorUserID          string                 `json:"actor_user_id"`
	Reason               string                 `json:"reason"`
	RequestID            string                 `json:"request_id"`
	RollbackFromRevision *int64                 `json:"rollback_from_revision,omitempty"`
	CreateTime           time.Time              `json:"create_time"`
}

type HistoryPage struct {
	Items      []HistoryView `json:"items"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor"`
}

type NamespaceStatus struct {
	Namespace         string `json:"namespace"`
	PersistedRevision int64  `json:"persisted_revision"`
	LoadedRevision    int64  `json:"loaded_revision"`
	ApplyState        string `json:"apply_state"`
}

type RuntimeStatus struct {
	BootID              string            `json:"boot_id"`
	BinarySHA256        string            `json:"binary_sha256"`
	PersistedGeneration int64             `json:"persisted_generation"`
	LoadedGeneration    int64             `json:"loaded_generation"`
	Namespaces          []NamespaceStatus `json:"namespaces"`
	LastRefreshAt       *time.Time        `json:"last_refresh_at"`
	LastError           string            `json:"last_error"`
	ApplyState          string            `json:"apply_state"`
}

type ValueChange struct {
	Key    string      `json:"key"`
	Before interface{} `json:"before"`
	After  interface{} `json:"after"`
}

type ValidationResult struct {
	Valid    bool                   `json:"valid"`
	Revision int64                  `json:"revision"`
	Values   map[string]interface{} `json:"values"`
	Changes  []ValueChange          `json:"changes"`
	Effects  []string               `json:"effects"`
}
