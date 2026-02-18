package orchestrator

import (
	"bytes"
	"context"
	"court-table-ai/pkg/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgentClient handles communication with AI providers
type AgentClient struct {
	client *http.Client
}

// NewAgentClient creates a new agent client
func NewAgentClient() *AgentClient {
	return &AgentClient{
		client: &http.Client{
			Timeout: 180 * time.Second, // Increased to 3 minutes
		},
	}
}

// OllamaRequest represents a request to Ollama API
type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// OllamaResponse represents a response from Ollama API
type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// OpenAIRequest represents a request to OpenAI-compatible API
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message represents a message in OpenAI format
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIResponse represents a response from OpenAI-compatible API
type OpenAIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice represents a choice in OpenAI response
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// AnthropicRequest represents a request to Anthropic Claude API
type AnthropicRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	Messages    []Message `json:"messages"`
	System      string    `json:"system,omitempty"`
}

// AnthropicResponse represents a response from Anthropic Claude API
type AnthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// GoogleRequest represents a request to Google Gemini API
type GoogleRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role,omitempty"`
	} `json:"contents"`
	SystemInstruction *struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"systemInstruction,omitempty"`
	GenerationConfig struct {
		Temperature     float64 `json:"temperature"`
		MaxOutputTokens int     `json:"maxOutputTokens"`
	} `json:"generationConfig"`
}

// GoogleResponse represents a response from Google Gemini API
type GoogleResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
			Role string `json:"role"`
		} `json:"content"`
		FinishReason  string `json:"finishReason"`
		Index         int    `json:"index"`
		SafetyRatings []struct {
			Category    string `json:"category"`
			Probability string `json:"probability"`
		} `json:"safetyRatings"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// CallAgent sends a request to an AI agent and returns the response
func (ac *AgentClient) CallAgent(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	startTime := time.Now()

	// Create context with timeout based on agent's configuration + buffer
	timeoutDuration := time.Duration(agent.TimeoutSeconds+10) * time.Second // Add 10s buffer
	timeoutCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	var response *models.AgentResponse
	var err error

	// Determine provider type and call appropriate method
	providerType := detectProviderType(agent.ProviderURL)

	fmt.Printf("Calling agent %s (%s) with timeout %v\n", agent.Name, providerType, timeoutDuration)

	switch providerType {
	case "ollama":
		response, err = ac.callOllama(timeoutCtx, agent, prompt, contextStr)
	case "openai":
		response, err = ac.callOpenAI(timeoutCtx, agent, prompt, contextStr)
	case "anthropic":
		response, err = ac.callAnthropic(timeoutCtx, agent, prompt, contextStr)
	case "google":
		response, err = ac.callGoogle(timeoutCtx, agent, prompt, contextStr)
	case "custom":
		response, err = ac.callCustom(timeoutCtx, agent, prompt, contextStr)
	default:
		// Default to custom for unknown providers
		response, err = ac.callCustom(timeoutCtx, agent, prompt, contextStr)
	}

	responseTime := int(time.Since(startTime).Milliseconds())
	if response != nil {
		response.ResponseTime = responseTime
	}

	if err != nil {
		fmt.Printf("Agent %s call failed: %v\n", agent.Name, err)
	}

	return response, err
}

// setAuthHeaders ensures consistent header setting across all methods
func (ac *AgentClient) setAuthHeaders(req *http.Request, agent *models.Agent) {
	providerType := detectProviderType(agent.ProviderURL)
	token := strings.TrimSpace(agent.APIToken)
	if token == "" {
		return
	}

	switch providerType {
	case "anthropic":
		req.Header.Set("x-api-key", token)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "google":
		req.Header.Set("x-goog-api-key", token)
	default:
		// OpenAI, Ollama, Custom
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// getChatEndpoints returns a prioritized list of chat endpoints for an agent
func (ac *AgentClient) getChatEndpoints(agentURL string) []string {
	baseURL := strings.TrimSuffix(strings.TrimSpace(agentURL), "/")

	// If it's already a full endpoint, use it directly
	if strings.Contains(baseURL, "/chat/completions") || strings.Contains(baseURL, "/generate") {
		return []string{baseURL}
	}

	// For OpenAI/Custom, try variants
	if strings.HasSuffix(baseURL, "/v1") {
		return []string{
			baseURL + "/chat/completions",
		}
	}

	return []string{
		baseURL + "/v1/chat/completions",
		baseURL + "/chat/completions",
		baseURL,
	}
}

// detectProviderType determines the provider type from URL
func detectProviderType(url string) string {
	if strings.Contains(url, "ollama") || strings.Contains(url, "localhost:11434") {
		return "ollama"
	} else if strings.Contains(url, "openai.com") {
		return "openai"
	} else if strings.Contains(url, "anthropic.com") {
		return "anthropic"
	} else if strings.Contains(url, "googleapis.com") {
		return "google"
	}
	// Default to custom for all other providers
	return "custom"
}

// callAnthropic calls Anthropic Claude API
func (ac *AgentClient) callAnthropic(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	// Build messages array for Claude
	var messages []Message

	// Add user message with context if available
	userMessage := prompt
	if contextStr != "" {
		userMessage = fmt.Sprintf("Previous context from other agents:\n%s\n\nYour task:\n%s", contextStr, prompt)
	}

	messages = append(messages, Message{
		Role:    "user",
		Content: userMessage,
	})

	// Build system message
	systemMessage := "You are participating in a multi-agent debate. Please provide thoughtful responses to the given topic."
	if contextStr != "" {
		systemMessage += " Consider the context from previous agents and provide your perspective or critique."
	}

	reqBody := AnthropicRequest{
		Model:       agent.ModelName,
		MaxTokens:   4000,
		Temperature: 0.7,
		Messages:    messages,
		System:      systemMessage,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to marshal request: %v", err),
		}, err
	}

	// Anthropic uses specific endpoint
	endpoint := agent.ProviderURL + "/messages"
	if !strings.Contains(agent.ProviderURL, "/v1") {
		endpoint = agent.ProviderURL + "/v1/messages"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to create request: %v", err),
		}, err
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Request failed: %v", err),
		}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read response: %v", err),
		}, err
	}

	if resp.StatusCode != http.StatusOK {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(body)),
		}, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var anthropicResp AnthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to unmarshal response: %v", err),
		}, err
	}

	if len(anthropicResp.Content) == 0 {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: "No content returned from Claude API",
		}, fmt.Errorf("no content in response")
	}

	// Extract text from first content block
	var content string
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}

	if content == "" {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: "No text content found in Claude response",
		}, fmt.Errorf("no text content")
	}

	return &models.AgentResponse{
		Success: true,
		Content: content,
	}, nil
}

// callCustom handles custom OpenAI-compatible APIs with better error handling
func (ac *AgentClient) callCustom(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	// First try OpenAI format
	response, err := ac.callOpenAI(ctx, agent, prompt, contextStr)
	if err == nil && response.Success {
		return response, nil
	}

	// If OpenAI format fails, try a more generic approach
	return ac.callGenericCompletion(ctx, agent, prompt, contextStr)
}

// callGenericCompletion tries a generic completion format for custom providers
func (ac *AgentClient) callGenericCompletion(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	// Build a simple completion request
	fullPrompt := prompt
	if contextStr != "" {
		fullPrompt = fmt.Sprintf("Previous context from other agents:\n%s\n\nYour task:\n%s", contextStr, prompt)
	}

	reqBody := map[string]interface{}{
		"prompt": fullPrompt,
		"model":  agent.ModelName,
		"stream": false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to marshal request: %v", err),
		}, err
	}

	// Try different endpoints for completion APIs
	baseURL := agent.ProviderURL
	if strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimSuffix(baseURL, "/v1")
	}

	// Try common endpoints for ping
	endpoints := []string{
		baseURL + "/v1/chat/completions",
	}

	for _, endpoint := range endpoints {
		response, err := ac.tryEndpoint(ctx, agent, endpoint, jsonData)
		if err == nil {
			return response, nil
		}
	}

	return &models.AgentResponse{
		Success:      false,
		ErrorMessage: "All endpoints failed for custom provider",
	}, fmt.Errorf("custom provider unreachable")
}

// tryEndpoint attempts to call an endpoint with the given request data
func (ac *AgentClient) tryEndpoint(ctx context.Context, agent *models.Agent, endpoint string, jsonData []byte) (*models.AgentResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Println("========HIT ENDPOINT===========")
	fmt.Println("req:", req)
	fmt.Println("endpoint:", endpoint)
	fmt.Println("payload:", string(jsonData))
	fmt.Println("response:", resp)
	fmt.Println("===================\n\n")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoint %s returned status %d: %s", endpoint, resp.StatusCode, string(body))
	}

	// Try to parse as generic JSON
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	// Extract text from various possible response formats
	content := ""
	if text, ok := result["text"].(string); ok {
		content = text
	} else if response, ok := result["response"].(string); ok {
		content = response
	} else if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if text, ok := message["content"].(string); ok {
					content = text
				}
			}
		}
	}

	if content == "" {
		return nil, fmt.Errorf("could not extract content from response")
	}

	return &models.AgentResponse{
		Success: true,
		Content: content,
	}, nil
}
func (ac *AgentClient) callGoogle(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	// Build contents for Gemini
	var contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role,omitempty"`
	}

	// Add system instruction if context is available
	var systemInstruction *struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}

	systemText := "You are participating in a multi-agent debate. Please provide thoughtful responses."
	if contextStr != "" {
		systemText += " Consider the context from previous agents and provide your perspective or critique."
	}

	systemInstruction = &struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: systemText}},
	}

	// Add user message with context
	userPrompt := prompt
	if contextStr != "" {
		userPrompt = fmt.Sprintf("Previous context from other agents:\n%s\n\nYour task:\n%s", contextStr, prompt)
	}

	contents = append(contents, struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
		Role string `json:"role,omitempty"`
	}{
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: userPrompt}},
		Role: "user",
	})

	reqBody := GoogleRequest{
		Contents:          contents,
		SystemInstruction: systemInstruction,
		GenerationConfig: struct {
			Temperature     float64 `json:"temperature"`
			MaxOutputTokens int     `json:"maxOutputTokens"`
		}{
			Temperature:     0.7,
			MaxOutputTokens: 4000,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to marshal request: %v", err),
		}, err
	}

	// Google Gemini endpoint format
	endpoint := agent.ProviderURL + "/models/" + agent.ModelName + ":generateContent"
	if !strings.Contains(agent.ProviderURL, "generativelanguage.googleapis.com") {
		// For custom endpoints
		endpoint = agent.ProviderURL + "/v1beta/generateContent"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to create request: %v", err),
		}, err
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Request failed: %v", err),
		}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read response: %v", err),
		}, err
	}

	if resp.StatusCode != http.StatusOK {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(body)),
		}, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var googleResp GoogleResponse
	if err := json.Unmarshal(body, &googleResp); err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to unmarshal response: %v", err),
		}, err
	}

	if len(googleResp.Candidates) == 0 {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: "No candidates returned from Gemini API",
		}, fmt.Errorf("no candidates")
	}

	candidate := googleResp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: "No content parts returned from Gemini API",
		}, fmt.Errorf("no content parts")
	}

	return &models.AgentResponse{
		Success: true,
		Content: candidate.Content.Parts[0].Text,
	}, nil
}
func (ac *AgentClient) callOllama(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	// Combine prompt and context
	fullPrompt := prompt
	if contextStr != "" {
		fullPrompt = fmt.Sprintf("Context from previous agents:\n%s\n\nYour task:\n%s", contextStr, prompt)
	}

	reqBody := OllamaRequest{
		Model:  agent.ModelName,
		Prompt: fullPrompt,
		Stream: false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to marshal request: %v", err),
		}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", agent.ProviderURL+"/api/generate", bytes.NewBuffer(jsonData))
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to create request: %v", err),
		}, err
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Request failed: %v", err),
		}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read response: %v", err),
		}, err
	}

	if resp.StatusCode != http.StatusOK {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(body)),
		}, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var ollamaResp OllamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to unmarshal response: %v", err),
		}, err
	}

	return &models.AgentResponse{
		Success: true,
		Content: ollamaResp.Response,
	}, nil
}

// callOpenAI calls an OpenAI-compatible API
func (ac *AgentClient) callOpenAI(ctx context.Context, agent *models.Agent, prompt string, contextStr string) (*models.AgentResponse, error) {
	// Build messages array
	var messages []Message

	// Add system message
	if contextStr != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: fmt.Sprintf("You are participating in a multi-agent debate. Here's the context from previous agents:\n%s\n\nPlease respond to the following:", contextStr),
		})
	} else {
		messages = append(messages, Message{
			Role:    "system",
			Content: "You are participating in a multi-agent debate. Please provide your response to the following:",
		})
	}

	// Add user message
	messages = append(messages, Message{
		Role:    "user",
		Content: prompt,
	})

	reqBody := OpenAIRequest{
		Model:    agent.ModelName,
		Messages: messages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return &models.AgentResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to marshal request: %v", err),
		}, err
	}

	// Determine the endpoint
	endpoints := ac.getChatEndpoints(agent.ProviderURL)
	var lastErr error
	
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		ac.setAuthHeaders(req, agent)

		resp, err := ac.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
			continue
		}

		var openaiResp OpenAIResponse
		if err := json.Unmarshal(body, &openaiResp); err != nil {
			lastErr = err
			continue
		}

		if len(openaiResp.Choices) == 0 {
			lastErr = fmt.Errorf("no choices in response")
			continue
		}

		return &models.AgentResponse{
			Success: true,
			Content: openaiResp.Choices[0].Message.Content,
		}, nil
	}

	return &models.AgentResponse{
		Success:      false,
		ErrorMessage: fmt.Sprintf("Failed to call OpenAI-compatible API: %v", lastErr),
	}, lastErr
}

// Ping checks if an agent is reachable
func (ac *AgentClient) Ping(ctx context.Context, agent *models.Agent) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	providerType := detectProviderType(agent.ProviderURL)

	switch providerType {
	case "ollama":
		return ac.pingOllama(timeoutCtx, agent)
	case "openai":
		return ac.pingOpenAI(timeoutCtx, agent)
	case "anthropic":
		return ac.pingAnthropic(timeoutCtx, agent)
	case "google":
		return ac.pingGoogle(timeoutCtx, agent)
	case "custom":
		return ac.pingCustom(timeoutCtx, agent)
	default:
		return ac.pingCustom(timeoutCtx, agent)
	}
}

// pingOllama handles Ollama-specific ping
func (ac *AgentClient) pingOllama(ctx context.Context, agent *models.Agent) error {
	endpoint := agent.ProviderURL + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return fmt.Errorf("ping failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return nil
}

// pingOpenAI handles OpenAI-specific ping
func (ac *AgentClient) pingOpenAI(ctx context.Context, agent *models.Agent) error {
	endpoint := agent.ProviderURL + "/models"
	if !strings.Contains(agent.ProviderURL, "/v1") {
		endpoint = agent.ProviderURL + "/v1/models"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return fmt.Errorf("ping failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return nil
}

// pingGoogle handles Google Gemini-specific ping
func (ac *AgentClient) pingGoogle(ctx context.Context, agent *models.Agent) error {
	endpoint := agent.ProviderURL + "/models"
	if !strings.Contains(agent.ProviderURL, "generativelanguage.googleapis.com") {
		endpoint = agent.ProviderURL + "/v1beta/models"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return fmt.Errorf("ping failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned status %d", resp.StatusCode)
	}

	return nil
}

// pingCustom handles custom provider ping with simple "hi" message
func (ac *AgentClient) pingCustom(ctx context.Context, agent *models.Agent) error {
	// Try simple completion request with "hi"
	reqBody := map[string]interface{}{
		"prompt": "hi",
		"model":  agent.ModelName,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create ping request: %v", err)
	}

	// Use unified endpoint detection
	endpoints := ac.getChatEndpoints(agent.ProviderURL)

	fmt.Printf("Pinging custom provider %s with endpoints: %v\n", agent.Name, endpoints)

	for i, endpoint := range endpoints {
		fmt.Printf("Trying endpoint %d/%d: %s\n", i+1, len(endpoints), endpoint)
		success := ac.tryPingEndpoint(ctx, agent, endpoint, jsonData)
		if success {
			fmt.Printf("Ping successful on endpoint: %s\n", endpoint)
			return nil
		} else {
			fmt.Printf("Ping failed on endpoint: %s\n", endpoint)
		}
	}

	return fmt.Errorf("custom provider ping failed - no endpoints responded")
}

// tryPingEndpoint attempts to ping an endpoint
func (ac *AgentClient) tryPingEndpoint(ctx context.Context, agent *models.Agent, endpoint string, jsonData []byte) bool {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return false
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	fmt.Println("===================")
	fmt.Println("Ping req:", req)
	fmt.Println("Ping endpoint:", endpoint)
	fmt.Println("Ping response:", resp)
	fmt.Println("===================\n\n\n")

	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Accept any 2xx status as success
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// pingAnthropic handles Anthropic-specific ping
func (ac *AgentClient) pingAnthropic(ctx context.Context, agent *models.Agent) error {
	// Create a minimal test message
	reqBody := AnthropicRequest{
		Model:     agent.ModelName,
		MaxTokens: 1,
		Messages:  []Message{{Role: "user", Content: "test"}},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create ping request: %v", err)
	}

	endpoint := agent.ProviderURL + "/messages"
	if !strings.Contains(agent.ProviderURL, "/v1") {
		endpoint = agent.ProviderURL + "/v1/messages"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	ac.setAuthHeaders(req, agent)

	resp, err := ac.client.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic ping failed: %v", err)
	}
	defer resp.Body.Close()

	// Accept 200 or 400 (invalid model) as success
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest {
		return nil
	}

	return fmt.Errorf("anthropic ping returned status %d", resp.StatusCode)
}
