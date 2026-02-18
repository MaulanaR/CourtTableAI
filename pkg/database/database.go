package database

import (
	"court-table-ai/pkg/models"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
}

// NewDB creates a new database connection
func NewDB(dataSourceName string) (*DB, error) {
	db, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// CreateTables creates all necessary tables for the application
func (db *DB) CreateTables() error {
	// Create agents table
	agentsSQL := `
	CREATE TABLE IF NOT EXISTS agents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		provider_url TEXT NOT NULL,
		api_token TEXT NOT NULL,
		model_name TEXT NOT NULL,
		timeout_seconds INTEGER DEFAULT 30,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	if _, err := db.Exec(agentsSQL); err != nil {
		return fmt.Errorf("failed to create agents table: %w", err)
	}

	// Create discussions table
	discussionsSQL := `
	CREATE TABLE IF NOT EXISTS discussions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		topic TEXT NOT NULL,
		final_summary TEXT NOT NULL DEFAULT '',
		status TEXT DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
		agent_ids TEXT NOT NULL,
		moderator_id INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (moderator_id) REFERENCES agents(id) ON DELETE SET NULL
	);`

	if _, err := db.Exec(discussionsSQL); err != nil {
		return fmt.Errorf("failed to create discussions table: %w", err)
	}

	// Create discussion_logs table
	discussionLogsSQL := `
	CREATE TABLE IF NOT EXISTS discussion_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		discussion_id INTEGER NOT NULL,
		agent_id INTEGER NOT NULL,
		content TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL CHECK (status IN ('success', 'timeout', 'error')),
		response_time INTEGER DEFAULT 0,
		is_moderator BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (discussion_id) REFERENCES discussions(id) ON DELETE CASCADE,
		FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
	);`

	if _, err := db.Exec(discussionLogsSQL); err != nil {
		return fmt.Errorf("failed to create discussion_logs table: %w", err)
	}

	// Create indexes for better performance
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_discussions_status ON discussions(status);",
		"CREATE INDEX IF NOT EXISTS idx_discussions_created_at ON discussions(created_at);",
		"CREATE INDEX IF NOT EXISTS idx_discussion_logs_discussion_id ON discussion_logs(discussion_id);",
		"CREATE INDEX IF NOT EXISTS idx_discussion_logs_agent_id ON discussion_logs(agent_id);",
		"CREATE INDEX IF NOT EXISTS idx_discussion_logs_created_at ON discussion_logs(created_at);",
	}

	for _, indexSQL := range indexes {
		if _, err := db.Exec(indexSQL); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	log.Println("Database tables created successfully")

	// Ensure new columns exist (Migration)
	// We use individual Exec calls and ignore errors because SQLite doesn't support 'IF NOT EXISTS' for columns
	db.Exec("ALTER TABLE discussions ADD COLUMN moderator_id INTEGER")
	db.Exec("ALTER TABLE discussion_logs ADD COLUMN is_moderator BOOLEAN DEFAULT FALSE")
	
	// Ensure these columns are NOT NULL for stability
	db.Exec("UPDATE discussions SET final_summary = '' WHERE final_summary IS NULL")
	db.Exec("UPDATE discussion_logs SET content = '' WHERE content IS NULL")

	return nil
}

// InsertAgent creates a new agent in the database
func (db *DB) InsertAgent(agent *models.Agent) error {
	query := `
	INSERT INTO agents (name, provider_url, api_token, model_name, timeout_seconds, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	
	now := time.Now()
	result, err := db.Exec(query, agent.Name, agent.ProviderURL, agent.APIToken, 
		agent.ModelName, agent.TimeoutSeconds, now, now)
	if err != nil {
		return fmt.Errorf("failed to insert agent: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	agent.ID = id
	agent.CreatedAt = now
	agent.UpdatedAt = now
	return nil
}

// GetAgent retrieves an agent by ID
func (db *DB) GetAgent(id int64) (*models.Agent, error) {
	query := `
	SELECT id, name, provider_url, api_token, model_name, timeout_seconds, created_at, updated_at
	FROM agents WHERE id = ?
	`
	
	agent := &models.Agent{}
	err := db.QueryRow(query, id).Scan(
		&agent.ID, &agent.Name, &agent.ProviderURL, &agent.APIToken,
		&agent.ModelName, &agent.TimeoutSeconds, &agent.CreatedAt, &agent.UpdatedAt,
	)
	
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	return agent, nil
}

// GetAllAgents retrieves all agents from the database
func (db *DB) GetAllAgents() ([]*models.Agent, error) {
	query := `
	SELECT id, name, provider_url, api_token, model_name, timeout_seconds, created_at, updated_at
	FROM agents ORDER BY created_at DESC
	`
	
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer rows.Close()

	var agents []*models.Agent
	for rows.Next() {
		agent := &models.Agent{}
		err := rows.Scan(
			&agent.ID, &agent.Name, &agent.ProviderURL, &agent.APIToken,
			&agent.ModelName, &agent.TimeoutSeconds, &agent.CreatedAt, &agent.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent: %w", err)
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

// UpdateAgent updates an existing agent
func (db *DB) UpdateAgent(agent *models.Agent) error {
	query := `
	UPDATE agents 
	SET name = ?, provider_url = ?, api_token = ?, model_name = ?, timeout_seconds = ?, updated_at = ?
	WHERE id = ?
	`
	
	agent.UpdatedAt = time.Now()
	result, err := db.Exec(query, agent.Name, agent.ProviderURL, agent.APIToken,
		agent.ModelName, agent.TimeoutSeconds, agent.UpdatedAt, agent.ID)
	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("agent not found")
	}

	return nil
}

// DeleteAgent deletes an agent by ID
func (db *DB) DeleteAgent(id int64) error {
	query := `DELETE FROM agents WHERE id = ?`
	
	result, err := db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("agent not found")
	}

	return nil
}

// InsertDiscussion creates a new discussion
func (db *DB) InsertDiscussion(discussion *models.Discussion) error {
	query := `
	INSERT INTO discussions (topic, final_summary, status, agent_ids, moderator_id, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	
	now := time.Now()
	result, err := db.Exec(query, discussion.Topic, discussion.FinalSummary, 
		discussion.Status, discussion.AgentIDs, discussion.ModeratorID, now, now)
	if err != nil {
		return fmt.Errorf("failed to insert discussion: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	discussion.ID = id
	discussion.CreatedAt = now
	discussion.UpdatedAt = now
	return nil
}

// GetDiscussion retrieves a discussion by ID
func (db *DB) GetDiscussion(id int64) (*models.Discussion, error) {
	query := `
	SELECT id, topic, COALESCE(final_summary, ''), status, agent_ids, moderator_id, created_at, updated_at
	FROM discussions WHERE id = ?
	`
	
	discussion := &models.Discussion{}
	err := db.QueryRow(query, id).Scan(
		&discussion.ID, &discussion.Topic, &discussion.FinalSummary,
		&discussion.Status, &discussion.AgentIDs, &discussion.ModeratorID, 
		&discussion.CreatedAt, &discussion.UpdatedAt,
	)
	
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("discussion not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get discussion: %w", err)
	}

	return discussion, nil
}

// GetAllDiscussions retrieves all discussions
func (db *DB) GetAllDiscussions() ([]*models.Discussion, error) {
	query := `
	SELECT id, topic, COALESCE(final_summary, ''), status, agent_ids, moderator_id, created_at, updated_at
	FROM discussions ORDER BY created_at DESC
	`
	
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query discussions: %w", err)
	}
	defer rows.Close()

	var discussions []*models.Discussion
	for rows.Next() {
		discussion := &models.Discussion{}
		err := rows.Scan(
			&discussion.ID, &discussion.Topic, &discussion.FinalSummary,
			&discussion.Status, &discussion.AgentIDs, &discussion.ModeratorID, 
			&discussion.CreatedAt, &discussion.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan discussion: %w", err)
		}
		discussions = append(discussions, discussion)
	}

	return discussions, nil
}

// InsertDiscussionLog creates a new discussion log entry
func (db *DB) InsertDiscussionLog(log *models.DiscussionLog) error {
	query := `
	INSERT INTO discussion_logs (discussion_id, agent_id, content, status, response_time, is_moderator, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	
	log.CreatedAt = time.Now()
	result, err := db.Exec(query, log.DiscussionID, log.AgentID, log.Content,
		log.Status, log.ResponseTime, log.IsModerator, log.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert discussion log: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	log.ID = id
	return nil
}

// GetDiscussionLogs retrieves all logs for a discussion
func (db *DB) GetDiscussionLogs(discussionID int64) ([]*models.DiscussionLog, error) {
	query := `
	SELECT id, discussion_id, agent_id, COALESCE(content, ''), status, response_time, is_moderator, created_at
	FROM discussion_logs WHERE discussion_id = ? ORDER BY created_at ASC
	`
	
	rows, err := db.Query(query, discussionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query discussion logs: %w", err)
	}
	defer rows.Close()

	var logs []*models.DiscussionLog
	for rows.Next() {
		log := &models.DiscussionLog{}
		err := rows.Scan(
			&log.ID, &log.DiscussionID, &log.AgentID, &log.Content,
			&log.Status, &log.ResponseTime, &log.IsModerator, &log.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan discussion log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// UpdateDiscussion updates a discussion
func (db *DB) UpdateDiscussion(discussion *models.Discussion) error {
	query := `
	UPDATE discussions 
	SET topic = ?, final_summary = ?, status = ?, agent_ids = ?, moderator_id = ?, updated_at = ?
	WHERE id = ?
	`
	
	discussion.UpdatedAt = time.Now()
	result, err := db.Exec(query, discussion.Topic, discussion.FinalSummary,
		discussion.Status, discussion.AgentIDs, discussion.ModeratorID, discussion.UpdatedAt, discussion.ID)
	if err != nil {
		return fmt.Errorf("failed to update discussion: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("discussion not found")
	}

	return nil
}

// DeleteDiscussion deletes a discussion by ID
func (db *DB) DeleteDiscussion(id int64) error {
	query := `DELETE FROM discussions WHERE id = ?`
	
	result, err := db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete discussion: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("discussion not found")
	}

	return nil
}
