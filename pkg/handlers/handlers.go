package handlers

import (
	"court-table-ai/pkg/database"
	"court-table-ai/pkg/models"
	"court-table-ai/pkg/orchestrator"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

type AgentHandler struct {
	db          *database.DB
	debateEngine *orchestrator.DebateEngine
}

// AgentRequest represents request payload for creating/updating agents
type AgentRequest struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	ProviderType  string      `json:"provider_type"`  // for frontend use only
	ProviderURL   string      `json:"provider_url"`
	APIToken      string      `json:"api_token"`
	ModelName     string      `json:"model_name"`
	TimeoutSeconds interface{} `json:"timeout_seconds"` // can be string or int
}

func NewAgentHandler(db *database.DB, debateEngine *orchestrator.DebateEngine) *AgentHandler {
	return &AgentHandler{
		db:          db,
		debateEngine: debateEngine,
	}
}

// CreateAgent handles POST /api/agents
func (h *AgentHandler) CreateAgent(c echo.Context) error {
	var req AgentRequest
	if err := c.Bind(&req); err != nil {
		fmt.Printf("Bind error: %v\n", err) // Debug log
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Invalid request body: %v", err)})
	}

	fmt.Printf("Received request: %+v\n", req) // Debug log

	// Validate required fields
	if req.Name == "" || req.ProviderURL == "" || req.ModelName == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "name, provider_url, and model_name are required"})
	}

	// Parse timeout_seconds - handle both string and int
	timeoutSeconds := 30 // default
	if req.TimeoutSeconds != nil {
		switch v := req.TimeoutSeconds.(type) {
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				timeoutSeconds = parsed
			}
		case float64:
			timeoutSeconds = int(v)
		case int:
			timeoutSeconds = v
		case int64:
			timeoutSeconds = int(v)
		}
	}

	// Convert request to model
	agent := models.Agent{
		Name:          req.Name,
		ProviderURL:   req.ProviderURL,
		APIToken:      req.APIToken,
		ModelName:     req.ModelName,
		TimeoutSeconds: timeoutSeconds,
	}

	if err := h.db.InsertAgent(&agent); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to create agent: %v", err)})
	}

	return c.JSON(http.StatusCreated, agent)
}

// GetAgents handles GET /api/agents
func (h *AgentHandler) GetAgents(c echo.Context) error {
	agents, err := h.db.GetAllAgents()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to get agents: %v", err)})
	}

	return c.JSON(http.StatusOK, agents)
}

// GetAgent handles GET /api/agents/:id
func (h *AgentHandler) GetAgent(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid agent ID"})
	}

	agent, err := h.db.GetAgent(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Agent not found"})
	}

	return c.JSON(http.StatusOK, agent)
}

// UpdateAgent handles PUT /api/agents/:id
func (h *AgentHandler) UpdateAgent(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid agent ID"})
	}

	var req AgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Invalid request body: %v", err)})
	}

	// Parse timeout_seconds - handle both string and int
	timeoutSeconds := 30 // default
	if req.TimeoutSeconds != nil {
		switch v := req.TimeoutSeconds.(type) {
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				timeoutSeconds = parsed
			}
		case float64:
			timeoutSeconds = int(v)
		case int:
			timeoutSeconds = v
		case int64:
			timeoutSeconds = int(v)
		}
	}

	// Convert request to model
	agent := models.Agent{
		ID:            id,
		Name:          req.Name,
		ProviderURL:   req.ProviderURL,
		APIToken:      req.APIToken,
		ModelName:     req.ModelName,
		TimeoutSeconds: timeoutSeconds,
	}

	if err := h.db.UpdateAgent(&agent); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to update agent: %v", err)})
	}

	return c.JSON(http.StatusOK, agent)
}

// DeleteAgent handles DELETE /api/agents/:id
func (h *AgentHandler) DeleteAgent(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid agent ID"})
	}

	if err := h.db.DeleteAgent(id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to delete agent: %v", err)})
	}

	return c.NoContent(http.StatusNoContent)
}

// DuplicateAgent handles POST /api/agents/:id/duplicate
func (h *AgentHandler) DuplicateAgent(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid agent ID"})
	}

	// Get original agent
	agent, err := h.db.GetAgent(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Agent not found"})
	}

	// Create duplicated agent with modified name
	duplicatedAgent := models.Agent{
		Name:           agent.Name + " - Copy",
		ProviderURL:    agent.ProviderURL,
		APIToken:       agent.APIToken,
		ModelName:      agent.ModelName,
		TimeoutSeconds: agent.TimeoutSeconds,
	}

	if err := h.db.InsertAgent(&duplicatedAgent); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to duplicate agent: %v", err)})
	}

	return c.JSON(http.StatusCreated, duplicatedAgent)
}
func (h *AgentHandler) PingAgent(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid agent ID"})
	}

	if err := h.debateEngine.PingAgent(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Ping failed: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// DiscussionHandler handles discussion-related endpoints
type DiscussionHandler struct {
	db          *database.DB
	debateEngine *orchestrator.DebateEngine
}

func NewDiscussionHandler(db *database.DB, debateEngine *orchestrator.DebateEngine) *DiscussionHandler {
	return &DiscussionHandler{
		db:          db,
		debateEngine: debateEngine,
	}
}

// CreateDiscussion handles POST /api/discussions
func (h *DiscussionHandler) CreateDiscussion(c echo.Context) error {
	var request struct {
		Topic       string  `json:"topic"`
		AgentIDs    []int64 `json:"agent_ids"`
		ModeratorID *int64  `json:"moderator_id"`
	}

	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	// Validate required fields
	if request.Topic == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "topic is required"})
	}

	if len(request.AgentIDs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "at least one agent is required"})
	}

	discussion, err := h.debateEngine.RunDebate(c.Request().Context(), request.Topic, request.AgentIDs, request.ModeratorID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to create discussion: %v", err)})
	}

	return c.JSON(http.StatusCreated, discussion)
}

// GetDiscussions handles GET /api/discussions
func (h *DiscussionHandler) GetDiscussions(c echo.Context) error {
	discussions, err := h.db.GetAllDiscussions()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to get discussions: %v", err)})
	}

	return c.JSON(http.StatusOK, discussions)
}

// GetDiscussion handles GET /api/discussions/:id
func (h *DiscussionHandler) GetDiscussion(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid discussion ID"})
	}

	discussion, logs, err := h.debateEngine.GetDiscussionStatus(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Discussion not found"})
	}

	response := map[string]interface{}{
		"discussion": discussion,
		"logs":       logs,
	}

	return c.JSON(http.StatusOK, response)
}

// StopDiscussion handles POST /api/discussions/:id/stop
func (h *DiscussionHandler) StopDiscussion(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid discussion ID"})
	}

	if err := h.debateEngine.StopDiscussion(id); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Failed to stop discussion: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "stopped"})
}

// DeleteDiscussion handles DELETE /api/discussions/:id
func (h *DiscussionHandler) DeleteDiscussion(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid discussion ID"})
	}

	if err := h.db.DeleteDiscussion(id); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to delete discussion: %v", err)})
	}

	return c.NoContent(http.StatusNoContent)
}

// RetryAgent handles POST /api/discussions/:id/retry/:agentId
func (h *DiscussionHandler) RetryAgent(c echo.Context) error {
	discussionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid discussion ID"})
	}

	agentID, err := strconv.ParseInt(c.Param("agentId"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid agent ID"})
	}

	if err := h.debateEngine.RetryFailedAgent(c.Request().Context(), discussionID, agentID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to retry agent: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "retry initiated"})
}

// SSEHandler handles Server-Sent Events for real-time updates
type SSEHandler struct {
	db          *database.DB
	debateEngine *orchestrator.DebateEngine
}

func NewSSEHandler(db *database.DB, debateEngine *orchestrator.DebateEngine) *SSEHandler {
	return &SSEHandler{
		db:          db,
		debateEngine: debateEngine,
	}
}

// StreamDiscussion handles GET /api/discussions/:id/stream
func (h *SSEHandler) StreamDiscussion(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid discussion ID"})
	}

	// Set SSE headers
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")

	// Get initial discussion status
	discussion, logs, err := h.debateEngine.GetDiscussionStatus(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Discussion not found"})
	}

	// Send initial data
	h.sendSSEUpdate(c.Response(), "discussion", discussion)
	h.sendSSEUpdate(c.Response(), "logs", logs)

	// For a complete implementation, you'd want to:
	// 1. Keep the connection open
	// 2. Poll for changes or use a notification system
	// 3. Send updates when new logs are added
	// 4. Handle client disconnection

	// For now, we'll just send a completion message
	h.sendSSEUpdate(c.Response(), "status", map[string]string{"message": "Streaming started"})

	return nil
}

func (h *SSEHandler) sendSSEUpdate(resp *echo.Response, eventType string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(resp, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
	resp.Flush()
	return err
}

// Page handlers for serving HTML
type PageHandler struct {
	db *database.DB
}

func NewPageHandler(db *database.DB) *PageHandler {
	return &PageHandler{db: db}
}

// Dashboard handles GET /
func (h *PageHandler) Dashboard(c echo.Context) error {
	agents, err := h.db.GetAllAgents()
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading agents</h1>")
	}

	discussions, err := h.db.GetAllDiscussions()
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading discussions</h1>")
	}

	data := map[string]interface{}{
		"Agents":      agents,
		"Discussions": discussions,
	}

	return c.Render(http.StatusOK, "dashboard.html", data)
}

// AgentsPage handles GET /agents
func (h *PageHandler) AgentsPage(c echo.Context) error {
	agents, err := h.db.GetAllAgents()
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading agents</h1>")
	}

	data := map[string]interface{}{
		"Agents": agents,
	}

	return c.Render(http.StatusOK, "agents.html", data)
}

// DiscussionsPage handles GET /discussions
func (h *PageHandler) DiscussionsPage(c echo.Context) error {
	discussions, err := h.db.GetAllDiscussions()
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading discussions</h1>")
	}

	agents, err := h.db.GetAllAgents()
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading agents</h1>")
	}

	data := map[string]interface{}{
		"Discussions": discussions,
		"Agents":      agents,
	}

	return c.Render(http.StatusOK, "discussions.html", data)
}

// DiscussionDetail handles GET /discussions/:id
func (h *PageHandler) DiscussionDetail(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.HTML(http.StatusBadRequest, "<h1>Invalid discussion ID</h1>")
	}

	discussion, err := h.db.GetDiscussion(id)
	if err != nil {
		return c.HTML(http.StatusNotFound, "<h1>Discussion not found</h1>")
	}

	logs, err := h.db.GetDiscussionLogs(id)
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading discussion logs</h1>")
	}

	agents, err := h.db.GetAllAgents()
	if err != nil {
		return c.HTML(http.StatusInternalServerError, "<h1>Error loading agents</h1>")
	}

	data := map[string]interface{}{
		"Discussion": discussion,
		"Logs":       logs,
		"Agents":     agents,
	}

	return c.Render(http.StatusOK, "discussion_detail.html", data)
}
