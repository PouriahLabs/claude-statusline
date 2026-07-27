// Package input models the JSON payload Claude Code pipes to a statusLine
// command on stdin.
//
// The schema below was captured from Claude Code 2.1.218. Every field is
// optional in practice: Claude Code omits sections that aren't populated yet
// (there is no cost object before the first API call, no rate_limits on some
// plans), so every consumer must handle zero values rather than assume
// presence.
package input

import (
	"encoding/json"
	"io"
)

type Payload struct {
	SessionID   string      `json:"session_id"`
	Version     string      `json:"version"`
	CWD         string      `json:"cwd"`
	Model       Model       `json:"model"`
	Workspace   Workspace   `json:"workspace"`
	Effort      Effort      `json:"effort"`
	Cost        Cost        `json:"cost"`
	Context     *Context    `json:"context_window"`
	RateLimits  *RateLimits `json:"rate_limits"`
	FastMode    bool        `json:"fast_mode"`
	OutputStyle struct {
		Name string `json:"name"`
	} `json:"output_style"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
	Repo       *struct {
		Host  string `json:"host"`
		Owner string `json:"owner"`
		Name  string `json:"name"`
	} `json:"repo"`
}

type Effort struct {
	Level string `json:"level"`
}

type Cost struct {
	TotalCostUSD     float64 `json:"total_cost_usd"`
	TotalDurationMS  int64   `json:"total_duration_ms"`
	TotalAPIDuration int64   `json:"total_api_duration_ms"`
	LinesAdded       int     `json:"total_lines_added"`
	LinesRemoved     int     `json:"total_lines_removed"`
}

type Context struct {
	TotalInputTokens  int64 `json:"total_input_tokens"`
	TotalOutputTokens int64 `json:"total_output_tokens"`
	// ContextWindowSize is reported as 200000 even for models whose real
	// window is 1M. Treated as a fallback only -- see config.ModelWindows.
	ContextWindowSize int64   `json:"context_window_size"`
	UsedPercentage    float64 `json:"used_percentage"`
}

type Window struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

type RateLimits struct {
	FiveHour *Window `json:"five_hour"`
	SevenDay *Window `json:"seven_day"`
}

// Parse reads a payload from r. A malformed or empty payload yields a zero
// Payload and no error: a status line must never print a parse error into the
// user's terminal, it should just render less.
func Parse(r io.Reader) Payload {
	var p Payload
	b, err := io.ReadAll(r)
	if err != nil || len(b) == 0 {
		return p
	}
	_ = json.Unmarshal(b, &p)
	return p
}

// Dir returns the working directory, preferring the workspace value.
func (p Payload) Dir() string {
	if p.Workspace.CurrentDir != "" {
		return p.Workspace.CurrentDir
	}
	return p.CWD
}
