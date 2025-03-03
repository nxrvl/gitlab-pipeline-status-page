package models

import (
	"time"

	"github.com/uptrace/bun"
)

// Pipeline represents a simplified GitLab pipeline.
type Pipeline struct {
	ID        int       `json:"id"`
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	WebURL    string    `json:"web_url"`
}

// Group represents a GitLab group.
type Group struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	FullPath    string    `json:"full_path"`
	Description string    `json:"description"`
	WebURL      string    `json:"web_url"`
	ParentID    int       `json:"parent_id"`
	Subgroups   []Group   `json:"-"`
	Projects    []Project `json:"-"`
	Level       int       `json:"-"` // For tree indentation
	HasChildren bool      `json:"-"` // Has subgroups or projects
	Expanded    bool      `json:"-"` // UI state
	Selected    bool      `json:"-"` // Used for UI selection
}

// Project represents a GitLab project.
type Project struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	NameWithNamespace string `json:"name_with_namespace"`
	Path              string `json:"path"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	Namespace         struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Path     string `json:"path"`
		FullPath string `json:"full_path"`
		Kind     string `json:"kind"`
	} `json:"namespace"`
	Selected bool `json:"-"` // Used for UI selection
	Level    int  `json:"-"` // For tree indentation
}

// User represents an application user.
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`

	ID        int64     `bun:"id,pk,autoincrement"`
	Username  string    `bun:"username,unique,notnull"`
	Password  string    `bun:"password,notnull"` // Hashed password
	GitLabURL string    `bun:"gitlab_url"`       // Optional custom GitLab URL for user
	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:current_timestamp"`
}

// RepositoryStatus holds the data to be displayed for each repository.
type RepositoryStatus struct {
	RepositoryID        int
	RepositoryName      string
	RepositoryPath      string
	Version             string
	PipelineID          int
	Status              string
	Date                time.Time
	WebURL              string
	LastSuccessPipeline *Pipeline
	RecentPipelines     []Pipeline // Last 10 pipelines for hover view
	ProjectURL          string
}

// SessionData holds the data stored in session
type SessionData struct {
	IsLoggedIn bool
	Username   string
	UserID     int64
}

// SelectedProject represents a project selected by a user to be displayed
type SelectedProject struct {
	bun.BaseModel `bun:"table:selected_projects,alias:sp"`

	ID        int64     `bun:"id,pk,autoincrement"`
	UserID    int64     `bun:"user_id,notnull"`
	ProjectID int       `bun:"project_id,notnull"`
	Path      string    `bun:"path,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp"`
}

// CachedProject represents a cached project from GitLab
type CachedProject struct {
	bun.BaseModel `bun:"table:cached_projects,alias:cp"`

	ID                int       `bun:"id,pk"` // GitLab project ID
	UserID            int64     `bun:"user_id,notnull"`
	Name              string    `bun:"name,notnull"`
	NameWithNamespace string    `bun:"name_with_namespace,notnull"`
	Path              string    `bun:"path,notnull"`
	PathWithNamespace string    `bun:"path_with_namespace,notnull"`
	WebURL            string    `bun:"web_url,notnull"`
	GroupID           int       `bun:"group_id"` // Parent group ID
	CreatedAt         time.Time `bun:"created_at,notnull,default:current_timestamp"`
	UpdatedAt         time.Time `bun:"updated_at,notnull,default:current_timestamp"`
}

// CachedGroup represents a cached group from GitLab
type CachedGroup struct {
	bun.BaseModel `bun:"table:cached_groups,alias:cg"`

	ID        int       `bun:"id,pk"` // GitLab group ID
	UserID    int64     `bun:"user_id,notnull"`
	Name      string    `bun:"name,notnull"`
	Path      string    `bun:"path,notnull"`
	FullPath  string    `bun:"full_path,notnull"`
	ParentID  int       `bun:"parent_id"` // Parent group ID
	WebURL    string    `bun:"web_url,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:current_timestamp"`
}