package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"golang.org/x/crypto/bcrypt"

	"gitlab-status/models"
)

// DB is the global database instance
var DB *bun.DB

// Initialize initializes the database
func Initialize(dbPath string) error {
	// Initialize SQLite database with Bun
	sqldb, err := sql.Open(sqliteshim.ShimName, dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}

	// Set a reasonable connection pool size
	sqldb.SetMaxOpenConns(1) // SQLite doesn't support multiple writers
	sqldb.SetMaxIdleConns(1)
	sqldb.SetConnMaxLifetime(time.Hour)

	// Create Bun instance using SQLite dialect
	DB = bun.NewDB(sqldb, sqlitedialect.New())

	// Create tables if they don't exist
	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %v", err)
	}

	return nil
}

// createTables creates the database tables if they don't exist
func createTables() error {
	// Create tables if they don't exist (don't reset the database on start)
	for _, model := range []interface{}{
		(*models.User)(nil),
		(*models.SelectedProject)(nil),
		(*models.CachedProject)(nil),
		(*models.CachedGroup)(nil),
	} {
		_, err := DB.NewCreateTable().Model(model).IfNotExists().Exec(context.Background())
		if err != nil {
			return fmt.Errorf("failed to create table for %T: %v", model, err)
		}
	}
	return nil
}

// CreateDefaultUser creates a default user if no users exist
func CreateDefaultUser(username, password string) error {
	// Check if any users exist
	count, err := DB.NewSelect().Model((*models.User)(nil)).Count(context.Background())
	if err != nil {
		return fmt.Errorf("failed to check users: %v", err)
	}

	// If no users exist, create the default user
	if count == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %v", err)
		}

		initialUser := models.User{
			Username:  username,
			Password:  string(hashedPassword),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = DB.NewInsert().Model(&initialUser).Exec(context.Background())
		if err != nil {
			return fmt.Errorf("failed to create initial user: %v", err)
		}
		log.Println("Created initial admin user")
	}

	return nil
}

// CacheGitLabStructure stores GitLab data in the database
func CacheGitLabStructure(groups []models.Group, projects []models.Project) error {
	ctx := context.Background()

	// Start a transaction
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	// Clear existing cached groups for all users - make data available to everyone
	// FIX: Add a "where true" condition to satisfy BUN's requirement for a WHERE clause
	_, err = tx.NewDelete().Model((*models.CachedGroup)(nil)).Where("1 = 1").Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to clear cached groups: %v", err)
	}

	// Insert all groups (without user ID - available to all users)
	for _, group := range groups {
		cachedGroup := models.CachedGroup{
			ID:        group.ID,
			UserID:    0, // 0 means available to all users
			Name:      group.Name,
			Path:      group.Path,
			FullPath:  group.FullPath,
			ParentID:  group.ParentID,
			WebURL:    group.WebURL,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = tx.NewInsert().Model(&cachedGroup).Exec(ctx)
		if err != nil {
			log.Printf("Error saving group %s: %v", group.Name, err)
		}
	}

	// Clear existing cached projects for all users - make data available to everyone
	// FIX: Add a "where true" condition to satisfy BUN's requirement for a WHERE clause
	_, err = tx.NewDelete().Model((*models.CachedProject)(nil)).Where("1 = 1").Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to clear cached projects: %v", err)
	}

	// Insert all projects (without user ID - available to all users)
	for _, project := range projects {
		cachedProject := models.CachedProject{
			ID:                project.ID,
			UserID:            0, // 0 means available to all users
			Name:              project.Name,
			NameWithNamespace: project.NameWithNamespace,
			Path:              project.Path,
			PathWithNamespace: project.PathWithNamespace,
			WebURL:            project.WebURL,
			GroupID:           project.Namespace.ID,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}

		_, err = tx.NewInsert().Model(&cachedProject).Exec(ctx)
		if err != nil {
			log.Printf("Error saving project %s: %v", project.Name, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

// GetSelectedProjects returns the selected projects for a user
func GetSelectedProjects(userID int64) ([]models.SelectedProject, error) {
	var selectedProjects []models.SelectedProject
	err := DB.NewSelect().Model(&selectedProjects).Where("user_id = ?", userID).Scan(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error fetching selected projects: %v", err)
	}
	return selectedProjects, nil
}

// GetCachedProject returns a cached project from the database
func GetCachedProject(projectID int) (*models.CachedProject, error) {
	var cachedProject models.CachedProject
	err := DB.NewSelect().Model(&cachedProject).Where("id = ?", projectID).Scan(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error fetching project from cache for ID %d: %v", projectID, err)
	}
	return &cachedProject, nil
}

// GetCachedGroups returns all cached groups from the database
func GetCachedGroups() ([]models.CachedGroup, error) {
	var cachedGroups []models.CachedGroup
	err := DB.NewSelect().Model(&cachedGroups).Order("name ASC").Scan(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error loading groups from cache: %v", err)
	}
	return cachedGroups, nil
}

// GetCachedProjects returns all cached projects from the database
func GetCachedProjects() ([]models.CachedProject, error) {
	var cachedProjects []models.CachedProject
	err := DB.NewSelect().Model(&cachedProjects).Order("name ASC").Scan(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error loading projects from cache: %v", err)
	}
	return cachedProjects, nil
}

// SaveSelectedProjects saves the selected projects for a user
func SaveSelectedProjects(userID int64, selectedIDs []string) error {
	ctx := context.Background()

	// Begin a transaction
	tx, err := DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	// Delete all existing selections for this user
	_, err = tx.NewDelete().Model((*models.SelectedProject)(nil)).Where("user_id = ?", userID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update settings: %v", err)
	}

	// Add new selections
	for _, idStr := range selectedIDs {
		var projectID int
		_, err := fmt.Sscanf(idStr, "%d", &projectID)
		if err != nil {
			continue
		}

		// Get project details from cache
		var cachedProject models.CachedProject
		err = tx.NewSelect().Model(&cachedProject).Where("id = ?", projectID).Scan(ctx)
		if err != nil {
			log.Printf("Error fetching project from cache: %v", err)
			continue
		}

		// Create new selection
		sp := models.SelectedProject{
			UserID:    userID,
			ProjectID: projectID,
			Path:      cachedProject.PathWithNamespace,
			CreatedAt: time.Now(),
		}

		_, err = tx.NewInsert().Model(&sp).Exec(ctx)
		if err != nil {
			log.Printf("Error saving project selection: %v", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to save settings: %v", err)
	}

	return nil
}

// GetUserByName returns a user by username
func GetUserByName(username string) (*models.User, error) {
	var user models.User
	err := DB.NewSelect().Model(&user).Where("username = ?", username).Scan(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error fetching user: %v", err)
	}
	return &user, nil
}

// CountCachedItems returns the count of cached projects and groups
func CountCachedItems() (int, int, error) {
	ctx := context.Background()
	projectCount, err := DB.NewSelect().Model((*models.CachedProject)(nil)).Count(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count cached projects: %v", err)
	}

	groupCount, err := DB.NewSelect().Model((*models.CachedGroup)(nil)).Count(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count cached groups: %v", err)
	}

	return projectCount, groupCount, nil
}
