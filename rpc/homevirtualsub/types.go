package homevirtualsub

type Worker struct {
	Name          string `json:"name"`
	BindIP        string `json:"bind_ip"`
	HTTPPort      int    `json:"http_port"`
	SocketPort    int    `json:"socket_port"`
	URLCount      int    `json:"url_count"`
	Enabled       bool   `json:"enabled"`
	Running       bool   `json:"running"`
	State         string `json:"state"`
	LastError     string `json:"last_error"`
	LastRefreshAt string `json:"last_refresh_at"`
	LastSuccessAt string `json:"last_success_at"`
}

type Status struct {
	Enabled        bool     `json:"enabled"`
	APIVersion     int      `json:"api_version"`
	StartedAt      string   `json:"started_at"`
	CoreType       string   `json:"core_type"`
	IntervalSecond int      `json:"interval_second"`
	Revision       string   `json:"revision"`
	Total          int      `json:"total"`
	Running        int      `json:"running"`
	Stopped        int      `json:"stopped"`
	Workers        []Worker `json:"workers"`
}

type Subscription struct {
	Name                string `json:"name"`
	BindIP              string `json:"bind_ip"`
	HTTPPort            int    `json:"http_port"`
	SocketPort          int    `json:"socket_port"`
	OutboundServerCount int    `json:"outbound_server_count"`
	URLCount            int    `json:"url_count"`
	HasProxyChain       bool   `json:"has_proxy_chain"`
}

type SubscriptionList struct {
	Revision      string         `json:"revision"`
	Subscriptions []Subscription `json:"subscriptions"`
}

type SubscriptionDetail struct {
	Revision     string `json:"revision"`
	Subscription struct {
		Subscription
		URLs []string `json:"urls"`
	} `json:"subscription"`
}

// Pointers preserve the distinction between an omitted field and an explicit
// zero in PATCH. URLs are required to remain nonempty whenever supplied.
type SubscriptionInput struct {
	Name                string   `json:"name,omitempty"`
	URLs                []string `json:"urls,omitempty"`
	BindIP              *string  `json:"bind_ip,omitempty"`
	HTTPPort            *int     `json:"http_port,omitempty"`
	SocketPort          *int     `json:"socket_port,omitempty"`
	OutboundServerCount *int     `json:"outbound_server_count,omitempty"`
}

type MutationResult struct {
	Saved         bool   `json:"saved"`
	Revision      string `json:"revision"`
	ApplyState    string `json:"apply_state"`
	Message       string `json:"message"`
	AuditRecorded bool   `json:"audit_recorded"`
}
