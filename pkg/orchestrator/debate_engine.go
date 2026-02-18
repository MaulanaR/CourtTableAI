package orchestrator

import (
	"context"
	"court-table-ai/pkg/database"
	"court-table-ai/pkg/models"
	"fmt"
	"log"
	"strings"
	"sync"
)

// DebateEngine orchestrates the debate between multiple AI agents
type DebateEngine struct {
	db          *database.DB
	agentClient *AgentClient
	subscribers map[int64][]chan interface{}
	subMu       sync.RWMutex
}

// NewDebateEngine creates a new debate engine
func NewDebateEngine(db *database.DB) *DebateEngine {
	return &DebateEngine{
		db:          db,
		agentClient: NewAgentClient(),
		subscribers: make(map[int64][]chan interface{}),
	}
}

// Subscribe adds a subscriber for a discussion
func (de *DebateEngine) Subscribe(discussionID int64) chan interface{} {
	de.subMu.Lock()
	defer de.subMu.Unlock()

	ch := make(chan interface{}, 10)
	de.subscribers[discussionID] = append(de.subscribers[discussionID], ch)
	return ch
}

// Unsubscribe removes a subscriber
func (de *DebateEngine) Unsubscribe(discussionID int64, ch chan interface{}) {
	de.subMu.Lock()
	defer de.subMu.Unlock()

	subs := de.subscribers[discussionID]
	for i, sub := range subs {
		if sub == ch {
			de.subscribers[discussionID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

// broadcast sends an update to all subscribers of a discussion
func (de *DebateEngine) broadcast(discussionID int64, data interface{}) {
	de.subMu.RLock()
	defer de.subMu.RUnlock()

	for _, ch := range de.subscribers[discussionID] {
		select {
		case ch <- data:
		default:
			// Buffer full, skip
		}
	}
}

// RunDebate starts a debate session with the specified topic and agents
func (de *DebateEngine) RunDebate(ctx context.Context, topic string, agentIDs []int64, moderatorID *int64, maxRounds int, language string, maxCharLimit int) (*models.Discussion, error) {
	// 1. Verify agents exist BEFORE creating discussion
	agents, err := de.getAgents(agentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to verify agents: %w", err)
	}

	var moderator *models.Agent
	if moderatorID != nil {
		moderator, err = de.db.GetAgent(*moderatorID)
		if err != nil {
			return nil, fmt.Errorf("failed to verify moderator: %w", err)
		}
	}

	// 2. Create discussion record
	discussion := &models.Discussion{
		Topic:        topic,
		Status:       "running",
		AgentIDs:     models.JSONSlice[int64](agentIDs),
		ModeratorID:  moderatorID,
		MaxRounds:    maxRounds,
		Language:     language,
		MaxCharLimit: maxCharLimit,
	}

	if err := de.db.InsertDiscussion(discussion); err != nil {
		return nil, fmt.Errorf("failed to create discussion: %w", err)
	}

	// 3. Start debate in background goroutine
	// Use background context so it doesn't get cancelled when HTTP request finishes
	go de.executeDebate(context.Background(), discussion, agents, moderator)

	return discussion, nil
}

// executeDebate runs the actual debate logic
func (de *DebateEngine) executeDebate(ctx context.Context, discussion *models.Discussion, agents []*models.Agent, moderator *models.Agent) {
	defer func() {
		// Update discussion status when done
		if r := recover(); r != nil {
			log.Printf("Debate panicked: %v", r)
			discussion.Status = "failed"
		} else if discussion.Status == "running" {
			discussion.Status = "completed"
		}
		de.db.UpdateDiscussion(discussion)
	}()

	log.Printf("Starting debate for discussion %d with %d agents%s (Max Rounds: %d, Language: %s, Max Chars: %d)",
		discussion.ID, len(agents), func() string {
			if moderator != nil {
				return fmt.Sprintf(" and moderator: %s", moderator.Name)
			}
			return ""
		}(), discussion.MaxRounds, discussion.Language, discussion.MaxCharLimit)

	// Moderator opens the discussion if available
	if moderator != nil {
		if !de.callModerator(ctx, discussion, moderator, "opening", "") {
			log.Printf("Moderator failed to give opening remarks for discussion %d", discussion.ID)
		}
	}

	// Build debate context from previous responses
	var debateContext strings.Builder
	roundCount := 1
	maxRounds := discussion.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 3 // Default fallback
	}

	for round := 1; round <= maxRounds; round++ {
		roundActive := false
		log.Printf("Starting round %d for discussion %d", round, discussion.ID)

		// Each agent responds in sequence
		for i, agent := range agents {
			// Build prompt for this agent
			prompt := de.buildPrompt(discussion)
			if round > 1 {
				prompt = de.buildRoundPrompt(discussion, round, i+1, len(agents))
			}

			// Call the agent
			response, err := de.agentClient.CallAgent(ctx, agent, prompt, debateContext.String())

			// Log the interaction
			logEntry := &models.DiscussionLog{
				DiscussionID: discussion.ID,
				AgentID:      agent.ID,
				Status:       "success",
				ResponseTime: response.ResponseTime,
				IsModerator:  false,
			}

			if err != nil {
				log.Printf("Agent %s failed to respond: %v", agent.Name, err)
				logEntry.Status = "error"
				logEntry.Content = fmt.Sprintf("Error: %v", err)
			} else if !response.Success {
				log.Printf("Agent %s returned error: %s", agent.Name, response.ErrorMessage)
				logEntry.Status = "error"
				logEntry.Content = fmt.Sprintf("Error: %s", response.ErrorMessage)
			} else {
				log.Printf("Agent %s responded successfully (%d ms)", agent.Name, response.ResponseTime)
				content := response.Content
				
				// Strictly enforce character limit (hard truncation)
				if len(content) > discussion.MaxCharLimit {
					content = content[:discussion.MaxCharLimit]
				}
				
				logEntry.Content = content
				roundActive = true

				// Add to debate context for next agents
				if debateContext.Len() > 0 {
					debateContext.WriteString("\n\n")
				}
				debateContext.WriteString(fmt.Sprintf("Round %d - Agent %s (%d):", round, agent.Name, agent.ID))
				debateContext.WriteString("\n")
				debateContext.WriteString(content)
			}

			// Save the log entry
			if err := de.db.InsertDiscussionLog(logEntry); err != nil {
				log.Printf("Failed to save discussion log: %v", err)
			} else {
				// Broadcast the new log
				de.broadcast(discussion.ID, logEntry)
			}

			// Moderator provides commentary between agent responses if available
			if moderator != nil && i < len(agents)-1 {
				if !de.callModerator(ctx, discussion, moderator, "interim", response.Content) {
					log.Printf("Moderator failed to give interim commentary for discussion %d", discussion.ID)
				}
			}
		}

		// Moderator provides round summary if available
		if moderator != nil {
			if !de.callModerator(ctx, discussion, moderator, "round_summary", fmt.Sprintf("Round %d completed", round)) {
				log.Printf("Moderator failed to give round summary for discussion %d", discussion.ID)
			}
		}

		// If no agent responded successfully in this round, end the debate
		if !roundActive {
			log.Printf("No active responses in round %d, ending debate", round)
			break
		}

		roundCount++
	}

	// Moderator provides closing remarks if available
	if moderator != nil {
		if !de.callModerator(ctx, discussion, moderator, "closing", "") {
			log.Printf("Moderator failed to give closing remarks for discussion %d", discussion.ID)
		}
	}

	// Generate final summary
	summary := de.generateSummary(discussion.Topic, debateContext.String())
	discussion.FinalSummary = summary
	discussion.Status = "completed"
	de.db.UpdateDiscussion(discussion)

	// Broadcast discussion update
	de.broadcast(discussion.ID, discussion)

	log.Printf("Debate completed for discussion %d", discussion.ID)
}

// buildPrompt creates a prompt for an agent's first round
func (de *DebateEngine) buildPrompt(discussion *models.Discussion) string {
	var prompt strings.Builder

	prompt.WriteString(fmt.Sprintf("You are an agent in a multi-agent debate about: \"%s\"\n\n", discussion.Topic))
	prompt.WriteString(fmt.Sprintf("Language of discussion: %s\n", discussion.Language))
	prompt.WriteString(fmt.Sprintf("Maximum response length: %d characters\n\n", discussion.MaxCharLimit))
	prompt.WriteString("This is the first round. Please provide your initial perspective on this topic.\n\n")
	prompt.WriteString("Guidelines:\n")
	prompt.WriteString("- Provide a clear, thoughtful response\n")
	prompt.WriteString("- Consider multiple perspectives\n")
	prompt.WriteString("- Be specific and provide reasoning\n")
	prompt.WriteString(fmt.Sprintf("- DO NOT EXCEED %d CHARACTERS\n", discussion.MaxCharLimit))
	prompt.WriteString(fmt.Sprintf("- RESPOND ONLY IN %s\n", strings.ToUpper(discussion.Language)))

	return prompt.String()
}

// buildRoundPrompt creates a prompt for subsequent rounds
func (de *DebateEngine) buildRoundPrompt(discussion *models.Discussion, round int, agentNum int, totalAgents int) string {
	var prompt strings.Builder

	prompt.WriteString(fmt.Sprintf("This is Round %d of the debate about: \"%s\"\n\n", round, discussion.Topic))
	prompt.WriteString(fmt.Sprintf("Language of discussion: %s\n", discussion.Language))
	prompt.WriteString(fmt.Sprintf("Maximum response length: %d characters\n\n", discussion.MaxCharLimit))
	prompt.WriteString(fmt.Sprintf("You are Agent #%d. Please respond to the previous arguments from other agents.\n\n", agentNum))
	prompt.WriteString("Guidelines:\n")
	prompt.WriteString("- Address specific points made by other agents\n")
	prompt.WriteString("- Defend or modify your position based on new information\n")
	prompt.WriteString("- Find common ground where possible\n")
	prompt.WriteString("- Move the discussion toward resolution\n")
	prompt.WriteString(fmt.Sprintf("- DO NOT EXCEED %d CHARACTERS\n", discussion.MaxCharLimit))
	prompt.WriteString(fmt.Sprintf("- RESPOND ONLY IN %s\n", strings.ToUpper(discussion.Language)))

	return prompt.String()
}

// callModerator handles moderator interactions
func (de *DebateEngine) callModerator(ctx context.Context, discussion *models.Discussion, moderator *models.Agent, moderatorType string, contextStr string) bool {
	// Build moderator prompt based on type
	prompt := de.buildModeratorPrompt(discussion, moderatorType, contextStr)

	response, err := de.agentClient.CallAgent(ctx, moderator, prompt, "")

	// Log the moderator interaction
	logEntry := &models.DiscussionLog{
		DiscussionID: discussion.ID,
		AgentID:      moderator.ID,
		Status:       "success",
		ResponseTime: response.ResponseTime,
		IsModerator:  true,
	}

	if err != nil {
		log.Printf("Moderator %s failed to respond: %v", moderator.Name, err)
		logEntry.Status = "error"
		logEntry.Content = fmt.Sprintf("Moderator Error: %v", err)
	} else if !response.Success {
		log.Printf("Moderator %s returned error: %s", moderator.Name, response.ErrorMessage)
		logEntry.Status = "error"
		logEntry.Content = fmt.Sprintf("Moderator Error: %s", response.ErrorMessage)
	} else {
		log.Printf("Moderator %s responded successfully (%d ms)", moderator.Name, response.ResponseTime)
		logEntry.Content = fmt.Sprintf("[Moderator - %s]\n%s", de.getModeratorRole(moderatorType), response.Content)
	}

	// Save the moderator log entry
	if err := de.db.InsertDiscussionLog(logEntry); err != nil {
		log.Printf("Failed to save moderator log: %v", err)
	} else {
		// Broadcast the moderator log
		de.broadcast(discussion.ID, logEntry)
	}

	return logEntry.Status == "success"
}

// buildModeratorPrompt creates prompts for different moderator interactions
func (de *DebateEngine) buildModeratorPrompt(discussion *models.Discussion, moderatorType string, contextStr string) string {
	topic := discussion.Topic
	lang := discussion.Language
	limit := discussion.MaxCharLimit

	basePrompt := fmt.Sprintf("You are the moderator for a multi-agent debate on: \"%s\"\nLanguage: %s\nMax length: %d characters\n\n", topic, lang, limit)

	switch moderatorType {
	case "opening":
		return basePrompt + `Your role is to:
1. Welcome participants and set the tone
2. Briefly explain the debate format and rules
3. Remind agents to be respectful and constructive
4. Introduce the topic and initial considerations

Please provide a concise opening statement (2-3 paragraphs). 
RESPOND ONLY IN ` + strings.ToUpper(lang) + `. DO NOT EXCEED ` + fmt.Sprint(limit) + ` CHARACTERS.`

	case "interim":
		return basePrompt + `An agent just responded with:

"` + contextStr + `"

Your role is to:
1. Briefly acknowledge the key points made
2. Keep the discussion focused and on track
3. Encourage the next agent to build upon or challenge these points
4. Maintain a respectful and constructive tone

Please provide a brief moderation comment (1-2 paragraphs).
RESPOND ONLY IN ` + strings.ToUpper(lang) + `. DO NOT EXCEED ` + fmt.Sprint(limit) + ` CHARACTERS.`

	case "round_summary":
		return basePrompt + `Round completed.

Your role is to:
1. Summarize the key arguments and perspectives from this round
2. Highlight areas of agreement and disagreement
3. Point out any logical fallacies or particularly strong arguments
4. Set up the next round of discussion

Please provide a concise round summary (2-3 paragraphs).
RESPOND ONLY IN ` + strings.ToUpper(lang) + `. DO NOT EXCEED ` + fmt.Sprint(limit) + ` CHARACTERS.`

	case "closing":
		return basePrompt + `The debate has concluded. Your role is to:
1. Provide a balanced summary of all positions presented
2. Identify the strongest arguments and key insights
3. Highlight areas of consensus and remaining disagreement
4. Offer final thoughts on the topic and the quality of the discussion

Please provide a comprehensive closing statement (3-4 paragraphs).
RESPOND ONLY IN ` + strings.ToUpper(lang) + `. DO NOT EXCEED ` + fmt.Sprint(limit) + ` CHARACTERS.`

	default:
		return basePrompt + "Please provide appropriate moderation in " + lang + ". DO NOT EXCEED " + fmt.Sprint(limit) + " CHARACTERS."
	}
}

// getModeratorRole returns a human-readable role description
func (de *DebateEngine) getModeratorRole(moderatorType string) string {
	switch moderatorType {
	case "opening":
		return "Opening Remarks"
	case "interim":
		return "Interim Moderation"
	case "round_summary":
		return "Round Summary"
	case "closing":
		return "Closing Remarks"
	default:
		return "Moderation"
	}
}
func (de *DebateEngine) generateSummary(topic string, context string) string {
	if context == "" {
		return "No responses were generated during this debate."
	}

	// For now, create a simple summary. In a production system,
	// you might want to use another AI call to generate a better summary
	summary := fmt.Sprintf("Debate Summary for: %s\n\n", topic)
	summary += "The debate involved multiple AI agents discussing this topic. "
	summary += "Each agent provided their perspective and responded to others' arguments. "
	summary += "For detailed discussion, please review the individual agent responses.\n\n"

	// Add first few lines of actual discussion as preview
	lines := strings.Split(context, "\n")
	if len(lines) > 5 {
		summary += "Key points discussed:\n"
		for i := 0; i < 5 && i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "" {
				summary += "- " + strings.TrimSpace(lines[i]) + "\n"
			}
		}
		if len(lines) > 5 {
			summary += "... (see full discussion for more details)"
		}
	}

	return summary
}

// getAgents retrieves agent details from database
func (de *DebateEngine) getAgents(agentIDs []int64) ([]*models.Agent, error) {
	var agents []*models.Agent
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(agentIDs))

	for _, id := range agentIDs {
		wg.Add(1)
		go func(agentID int64) {
			defer wg.Done()
			agent, err := de.db.GetAgent(agentID)
			if err != nil {
				errChan <- err
				return
			}
			mu.Lock()
			agents = append(agents, agent)
			mu.Unlock()
		}(id)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return nil, err
		}
	}

	// Verify all agents were found
	if len(agents) != len(agentIDs) {
		return nil, fmt.Errorf("only found %d out of %d agents", len(agents), len(agentIDs))
	}

	return agents, nil
}

// PingAgent checks if an agent is reachable
func (de *DebateEngine) PingAgent(ctx context.Context, agentID int64) error {
	agent, err := de.db.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	return de.agentClient.Ping(ctx, agent)
}

// GetDiscussionStatus retrieves the current status of a discussion
func (de *DebateEngine) GetDiscussionStatus(discussionID int64) (*models.Discussion, []*models.DiscussionLog, error) {
	discussion, err := de.db.GetDiscussion(discussionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get discussion: %w", err)
	}

	logs, err := de.db.GetDiscussionLogs(discussionID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get discussion logs: %w", err)
	}

	return discussion, logs, nil
}

// StopDiscussion stops a running discussion
func (de *DebateEngine) StopDiscussion(discussionID int64) error {
	discussion, err := de.db.GetDiscussion(discussionID)
	if err != nil {
		return fmt.Errorf("failed to get discussion: %w", err)
	}

	if discussion.Status != "running" {
		return fmt.Errorf("discussion is not running")
	}

	discussion.Status = "completed"
	return de.db.UpdateDiscussion(discussion)
}

// RetryFailedAgent retries a failed agent response
func (de *DebateEngine) RetryFailedAgent(ctx context.Context, discussionID int64, agentID int64) error {
	// Get discussion and agent
	discussion, err := de.db.GetDiscussion(discussionID)
	if err != nil {
		return fmt.Errorf("failed to get discussion: %w", err)
	}

	if discussion.Status != "running" {
		return fmt.Errorf("discussion is not running")
	}

	agent, err := de.db.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	// Get previous logs to build context
	logs, err := de.db.GetDiscussionLogs(discussionID)
	if err != nil {
		return fmt.Errorf("failed to get discussion logs: %w", err)
	}

	// Build context from previous successful responses
	var contextBuilder strings.Builder
	for _, log := range logs {
		if log.Status == "success" {
			if contextBuilder.Len() > 0 {
				contextBuilder.WriteString("\n\n")
			}
			contextBuilder.WriteString(log.Content)
		}
	}

	// Retry the agent call
	prompt := de.buildPrompt(discussion) // Simplified prompt for retry
	response, err := de.agentClient.CallAgent(ctx, agent, prompt, contextBuilder.String())

	// Create new log entry
	logEntry := &models.DiscussionLog{
		DiscussionID: discussionID,
		AgentID:      agentID,
		Status:       "success",
		ResponseTime: response.ResponseTime,
	}

	if err != nil {
		logEntry.Status = "error"
		logEntry.Content = fmt.Sprintf("Retry failed: %v", err)
	} else if !response.Success {
		logEntry.Status = "error"
		logEntry.Content = fmt.Sprintf("Retry failed: %s", response.ErrorMessage)
	} else {
		logEntry.Content = response.Content
	}

	return de.db.InsertDiscussionLog(logEntry)
}
