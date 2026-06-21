package adminapi

import (
	"net/http"
	"time"
)

// healthResponse is the JSON shape for GET /api/health per contracts/admin-api.md.
type healthResponse struct {
	Status                  string   `json:"status"`
	Version                 string   `json:"version"`
	UptimeSeconds           int64    `json:"uptime_seconds"`
	LastSuccessfulCycleAt   *string  `json:"last_successful_cycle_at"`
	LastFailureAt           *string  `json:"last_failure_at"`
	LastFailureReason       *string  `json:"last_failure_reason"`
	SchedulerState          string   `json:"scheduler_state"`
	DBOk                    bool     `json:"db_ok"`
	ChannelStatuses         []chanSummary `json:"channel_statuses,omitempty"`
}

type chanSummary struct {
	Handle         string `json:"handle"`
	DisplayName    string `json:"display_name"`
	Status         string `json:"status"`
	LastObservedAt *string `json:"last_observed_at"`
	LastError      *string `json:"last_error"`
}

// handleHealth returns the liveness + summary of the last cycle.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	snap, err := s.deps.Health.Snapshot(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_unavailable", err.Error(), "")
		return
	}

	state := "idle"
	if s.deps.SchedulerState != nil {
		if v := s.deps.SchedulerState(); v != "" {
			state = v
		}
	}

	status := "ok"
	if len(snap.ChannelStatuses) > 0 {
		// If any channel is banned, surface as degraded but still ok.
		for _, c := range snap.ChannelStatuses {
			if string(c.Status) == "banned" {
				status = "degraded"
				break
			}
		}
	}

	resp := healthResponse{
		Status:         status,
		Version:        s.deps.Version,
		UptimeSeconds:  int64(time.Since(s.deps.StartedAt).Seconds()),
		SchedulerState: state,
		DBOk:           true,
	}
	if !snap.LastSuccessfulCycleAt.IsZero() {
		t := snap.LastSuccessfulCycleAt.UTC().Format(time.RFC3339)
		resp.LastSuccessfulCycleAt = &t
	}
	if !snap.LastFailureAt.IsZero() {
		t := snap.LastFailureAt.UTC().Format(time.RFC3339)
		resp.LastFailureAt = &t
	}
	if snap.LastFailureReason != "" {
		resp.LastFailureReason = &snap.LastFailureReason
	}
	for _, c := range snap.ChannelStatuses {
		row := chanSummary{
			Handle:      c.Handle,
			DisplayName: c.DisplayName,
			Status:      string(c.Status),
		}
		if !c.LastObservedAt.IsZero() {
			t := c.LastObservedAt.UTC().Format(time.RFC3339)
			row.LastObservedAt = &t
		}
		if c.LastError != "" {
			row.LastError = &c.LastError
		}
		resp.ChannelStatuses = append(resp.ChannelStatuses, row)
	}

	writeJSON(w, http.StatusOK, resp)
}
