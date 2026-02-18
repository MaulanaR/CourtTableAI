package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
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
	MaxRounds    int                `json:"max_rounds" db:"max_rounds"`
	Language     string             `json:"language" db:"language"`
	MaxCharLimit int                `json:"max_char_limit" db:"max_char_limit"`
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
	
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("unexpected type for JSONSlice: %T", value)
	}

	if len(data) == 0 {
		*j = JSONSlice[T]{}
		return nil
	}

	// Try to unmarshal as JSON
	err := json.Unmarshal(data, j)
	if err == nil {
		return nil
	}

	// Fallback for legacy or malformed data: try to parse as comma-separated if it's not valid JSON
	// This is a safety measure to prevent Internal Server Errors on older records
	strData := string(data)
	if !strings.HasPrefix(strData, "[") {
		parts := strings.Split(strData, ",")
		var result []T
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			var val T
			// Use JSON to convert the string to the target type T if possible
			// This works for basic types like int64 or string
			if err := json.Unmarshal([]byte(fmt.Sprintf("%q", p)), &val); err == nil {
				result = append(result, val)
			} else if err := json.Unmarshal([]byte(p), &val); err == nil {
				result = append(result, val)
			}
		}
		*j = result
		return nil
	}

	return fmt.Errorf("failed to unmarshal JSONSlice: %w", err)
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
