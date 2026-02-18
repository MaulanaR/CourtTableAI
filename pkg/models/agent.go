package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// Agent represents an AI agent configuration
type Agent struct {
	ID            int64     `json:"id" db:"id"`
	Name          string    `json:"name" db:"name"`
	ProviderType  string    `json:"provider_type" db:"provider_type"` // ollama, openai, anthropic, google, custom
	ProviderURL   string    `json:"provider_url" db:"provider_url"`
	APIToken      string    `json:"api_token" db:"api_token"`
	ModelName     string    `json:"model_name" db:"model_name"`
	TimeoutSeconds int      `json:"timeout_seconds" db:"timeout_seconds"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// Discussion represents a debate/discussion session
type Discussion struct {
	ID           int64              `json:"id" db:"id"`
	Topic        string             `json:"topic" db:"topic"`
	FinalSummary string             `json:"final_summary" db:"final_summary"`
	Status       string             `json:"status" db:"status"` // running, completed, failed
	AgentIDs     JSONSlice[int64]   `json:"agent_ids" db:"agent_ids"`
	ModeratorID  *int64             `json:"moderator_id" db:"moderator_id"` // nullable
	CreatedAt    time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at" db:"updated_at"`
}

// DiscussionLog represents individual agent responses in a discussion
type DiscussionLog struct {
	ID           int64     `json:"id" db:"id"`
	DiscussionID int64     `json:"discussion_id" db:"discussion_id"`
	AgentID      int64     `json:"agent_id" db:"agent_id"`
	Content      string    `json:"content" db:"content"`
	Status       string    `json:"status" db:"status"` // success, timeout, error
	ResponseTime int       `json:"response_time" db:"response_time"` // in milliseconds
	IsModerator  bool      `json:"is_moderator" db:"is_moderator"` // moderator role indicator
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// JSONSlice is a custom type for handling JSON arrays in database
type JSONSlice[T any] []T

func (j JSONSlice[T]) Value() (driver.Value, error) {
	return json.Marshal(j)
}

func (j *JSONSlice[T]) Scan(value interface{}) error {
	if value == nil {
		*j = JSONSlice[T]{}
		return nil
	}
	
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, j)
	case string:
		return json.Unmarshal([]byte(v), j)
	}
	return nil
}

// AgentRequest represents a request to an AI agent
type AgentRequest struct {
	Prompt   string            `json:"prompt"`
	Context  string            `json:"context,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AgentResponse represents a response from an AI agent
type AgentResponse struct {
	Content      string            `json:"content"`
	Success      bool              `json:"success"`
	ErrorMessage string            `json:"error_message,omitempty"`
	ResponseTime int               `json:"response_time"` // in milliseconds
	Metadata     map[string]string `json:"metadata,omitempty"`
}
