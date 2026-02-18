package main

import (
	"court-table-ai/pkg/database"
	"court-table-ai/pkg/handlers"
	"court-table-ai/pkg/orchestrator"
	"html/template"
	"io"
	"log"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type TemplateRenderer struct {
	templates *template.Template
}

func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func loadTemplates() *template.Template {
	templ := template.New("").Funcs(template.FuncMap{
		"len": func(v interface{}) int {
			switch val := v.(type) {
			case []interface{}:
				return len(val)
			case string:
				return len(val)
			case []string:
				return len(val)
			case []int64:
				return len(val)
			case map[string]interface{}:
				return len(val)
			default:
				return 0
			}
		},
		"substr": func(s string, start int, length ...int) string {
			if start < 0 {
				start = 0
			}
			if start >= len(s) {
				return ""
			}
			end := len(s)
			if len(length) > 0 && start+length[0] < len(s) {
				end = start + length[0]
			}
			return s[start:end]
		},
		"upper": func(s string) string {
			return strings.ToUpper(s)
		},
		"eq": func(a, b interface{}) bool {
			return a == b
		},
		"ne": func(a, b interface{}) bool {
			return a != b
		},
		"gt": func(a, b interface{}) bool {
			switch a.(type) {
			case int:
				return a.(int) > b.(int)
			case int64:
				return a.(int64) > b.(int64)
			case float64:
				return a.(float64) > b.(float64)
			}
			return false
		},
		"lt": func(a, b interface{}) bool {
			switch a.(type) {
			case int:
				return a.(int) < b.(int)
			case int64:
				return a.(int64) < b.(int64)
			case float64:
				return a.(float64) < b.(float64)
			}
			return false
		},
		"getProviderType": func(url string) string {
			if strings.Contains(url, "openai.com") {
				return "OpenAI"
			} else if strings.Contains(url, "anthropic.com") {
				return "Anthropic"
			} else if strings.Contains(url, "googleapis.com") {
				return "Google"
			} else if strings.Contains(url, "localhost:11434") || strings.Contains(url, "ollama") {
				return "Ollama"
			} else {
				return "Custom"
			}
		},
	})

	return template.Must(templ.ParseGlob("templates/*.html"))
}

func main() {
	// Initialize database
	db, err := database.NewDB("court_table_ai.db")
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	// Create tables
	if err := db.CreateTables(); err != nil {
		log.Fatal("Failed to create tables:", err)
	}

	// Initialize debate engine
	debateEngine := orchestrator.NewDebateEngine(db)

	// Initialize Echo
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Template renderer
	e.Renderer = &TemplateRenderer{
		templates: loadTemplates(),
	}

	// Static files
	e.Static("/static", "static")

	// Initialize handlers
	agentHandler := handlers.NewAgentHandler(db, debateEngine)
	discussionHandler := handlers.NewDiscussionHandler(db, debateEngine)
	sseHandler := handlers.NewSSEHandler(db, debateEngine)
	pageHandler := handlers.NewPageHandler(db)

	// API Routes
	api := e.Group("/api")
	
	// Agent routes
	api.POST("/agents", agentHandler.CreateAgent)
	api.GET("/agents", agentHandler.GetAgents)
	api.GET("/agents/:id", agentHandler.GetAgent)
	api.PUT("/agents/:id", agentHandler.UpdateAgent)
	api.DELETE("/agents/:id", agentHandler.DeleteAgent)
	api.POST("/agents/:id/ping", agentHandler.PingAgent)

	// Discussion routes
	api.POST("/discussions", discussionHandler.CreateDiscussion)
	api.GET("/discussions", discussionHandler.GetDiscussions)
	api.GET("/discussions/:id", discussionHandler.GetDiscussion)
	api.POST("/discussions/:id/stop", discussionHandler.StopDiscussion)
	api.POST("/discussions/:id/retry/:agentId", discussionHandler.RetryAgent)

	// SSE routes
	api.GET("/discussions/:id/stream", sseHandler.StreamDiscussion)

	// Page routes
	e.GET("/", pageHandler.Dashboard)
	e.GET("/agents", pageHandler.AgentsPage)
	e.GET("/discussions", pageHandler.DiscussionsPage)
	e.GET("/discussions/:id", pageHandler.DiscussionDetail)

	// Start server
	log.Println("Starting server on :8080")
	if err := e.Start(":8080"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
