package cronagent

type ScheduleStatus struct {
	Prompt        string `json:"prompt"`
	Cronexpr      string `json:"cronExpr"`
	Id            string `json:"id"`
	Err           string `json:"error,omitempty"`
	CreatedAt     string `json:"createdAt"`
	EnableAt      string `json:"enableAt,omitempty"`
	DisableAt     string `json:"disableAt,omitempty"`
	LastTriggerAt string `json:"lastTriggerAt,omitempty"`
	Cronid        int    `json:"cronId"`
	Paused        bool   `json:"paused"`
	Executing     bool   `json:"executing"`
}
