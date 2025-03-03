package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"

	"gitlab-status/db"
	"gitlab-status/gitlab"
	"gitlab-status/models"
)

// SettingsPageHandler handles the settings page request
func SettingsPageHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Check for action parameter (expand/collapse)
	action := c.QueryParam("action")
	groupIDStr := c.QueryParam("groupID")

	// Load all cached groups
	cachedGroups, err := db.GetCachedGroups()
	if err != nil {
		log.Printf("Error loading groups from cache: %v", err)
		return c.Render(http.StatusOK, "settings.html", map[string]interface{}{
			"Username": session.Values["username"],
			"ApiError": fmt.Sprintf("Error loading groups from cache: %v", err),
			"TreeView": true,
		})
	}

	// If we have no cached groups, show caching in progress message
	if len(cachedGroups) == 0 {
		log.Printf("No cached groups found for user %d, initiating cache...", userID)

		// Show caching in progress message
		return c.Render(http.StatusOK, "settings.html", map[string]interface{}{
			"Username": session.Values["username"],
			"ApiError": "Loading GitLab structure in the background. This may take a while. Please refresh in a few minutes.",
			"Caching":  true,
			"TreeView": true,
		})
	}

	// Convert cached groups to Group objects
	// First, create a map of all groups
	groupMap := make(map[int]models.Group)
	for _, cg := range cachedGroups {
		group := models.Group{
			ID:          cg.ID,
			Name:        cg.Name,
			Path:        cg.Path,
			FullPath:    cg.FullPath,
			WebURL:      cg.WebURL,
			ParentID:    cg.ParentID,
			Subgroups:   []models.Group{},
			Projects:    []models.Project{},
			Level:       0,
			HasChildren: false,
			Expanded:    true, // Default expanded
		}
		groupMap[group.ID] = group
	}

	// Load all cached projects
	cachedProjects, err := db.GetCachedProjects()
	if err != nil {
		log.Printf("Error loading projects from cache: %v", err)
	}

	// Assign projects to their parent groups
	for _, cp := range cachedProjects {
		project := models.Project{
			ID:                cp.ID,
			Name:              cp.Name,
			NameWithNamespace: cp.NameWithNamespace,
			Path:              cp.Path,
			PathWithNamespace: cp.PathWithNamespace,
			WebURL:            cp.WebURL,
			Level:             1, // Default level
		}

		// Set namespace info
		project.Namespace.ID = cp.GroupID
		project.Namespace.Path = cp.Path

		// Add to parent group
		if group, exists := groupMap[cp.GroupID]; exists {
			group.Projects = append(group.Projects, project)
			group.HasChildren = true
			groupMap[cp.GroupID] = group
		}
	}

	// Build the tree structure
	var groupTree []models.Group

	// First, identify top-level groups
	for _, group := range groupMap {
		if group.ParentID == 0 {
			groupTree = append(groupTree, group)
		} else {
			// Add as subgroup to parent
			if parent, exists := groupMap[group.ParentID]; exists {
				parent.Subgroups = append(parent.Subgroups, group)
				parent.HasChildren = true
				groupMap[parent.ID] = parent
			}
		}
	}

	// Set levels for groups and their projects
	for i := range groupTree {
		groupTree[i].Level = 0
		groupTree[i].Expanded = true // Top level groups are expanded by default

		// Set level for projects
		for j := range groupTree[i].Projects {
			groupTree[i].Projects[j].Level = 1
		}

		// Set level for subgroups recursively
		for j := range groupTree[i].Subgroups {
			groupTree[i].Subgroups[j].Level = 1

			// This is a simplified approach - for deep nesting,
			// you'd want to implement a recursive function
		}
	}

	// If this is an expand/collapse action, update the tree
	if action != "" && groupIDStr != "" {
		groupID, err := strconv.Atoi(groupIDStr)
		if err == nil {
			// Find the group and toggle its expanded state
			for i := range groupTree {
				if groupTree[i].ID == groupID {
					if action == "expand" {
						groupTree[i].Expanded = true
					} else if action == "collapse" {
						groupTree[i].Expanded = false
					}
					break
				}

				// Check subgroups (this is simplified - for deep nesting you'd want recursion)
				for j := range groupTree[i].Subgroups {
					if groupTree[i].Subgroups[j].ID == groupID {
						if action == "expand" {
							groupTree[i].Subgroups[j].Expanded = true
						} else if action == "collapse" {
							groupTree[i].Subgroups[j].Expanded = false
						}
						break
					}
				}
			}
		}
	}

	// Get currently selected projects from database
	selectedProjects, err := db.GetSelectedProjects(userID)
	if err != nil {
		log.Printf("Error fetching selected projects: %v", err)
	}

	// Create a map for faster lookup
	selectedProjectMap := make(map[int]bool)
	for _, sp := range selectedProjects {
		selectedProjectMap[sp.ProjectID] = true
	}

	// Mark selected projects in the tree
	for g := range groupTree {
		for p := range groupTree[g].Projects {
			if selectedProjectMap[groupTree[g].Projects[p].ID] {
				groupTree[g].Projects[p].Selected = true
			}
		}

		// Check subgroups too (simplified - for deep nesting you'd want recursion)
		for s := range groupTree[g].Subgroups {
			for p := range groupTree[g].Subgroups[s].Projects {
				if selectedProjectMap[groupTree[g].Subgroups[s].Projects[p].ID] {
					groupTree[g].Subgroups[s].Projects[p].Selected = true
				}
			}
		}
	}

	return c.Render(http.StatusOK, "settings.html", map[string]interface{}{
		"GroupTree": groupTree,
		"Username":  session.Values["username"],
		"TreeView":  true,
	})
}

// ProjectsPageHandler handles the projects page request
func ProjectsPageHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Check if we have cached projects
	projectCount, _, err := db.CountCachedItems()
	if err != nil {
		log.Printf("Error checking cached projects: %v", err)
	}

	// If we don't have cached projects, show message to refresh
	if projectCount == 0 {
		log.Printf("No cached projects found in database")

		// Show message to user
		return c.Render(http.StatusOK, "settings.html", map[string]interface{}{
			"Username": session.Values["username"],
			"ApiError": "No projects found in database. Click Refresh Data to load GitLab projects.",
			"TreeView": false,
			"Caching":  true,
		})
	}

	// Get projects from cache
	log.Printf("Loading projects from cache")
	startTime := time.Now()

	// Load projects from cache
	cachedProjects, err := db.GetCachedProjects()
	if err != nil {
		log.Printf("Error loading projects from cache: %v", err)
		return c.Render(http.StatusOK, "settings.html", map[string]interface{}{
			"Username": session.Values["username"],
			"ApiError": "Failed to load projects from cache: " + err.Error(),
			"TreeView": false,
		})
	}

	// Convert cached projects to Project objects
	var allProjects []models.Project
	for _, cp := range cachedProjects {
		project := models.Project{
			ID:                cp.ID,
			Name:              cp.Name,
			NameWithNamespace: cp.NameWithNamespace,
			Path:              cp.Path,
			PathWithNamespace: cp.PathWithNamespace,
			WebURL:            cp.WebURL,
		}

		// Set namespace info
		project.Namespace.ID = cp.GroupID
		project.Namespace.Path = cp.Path

		allProjects = append(allProjects, project)
	}

	log.Printf("Successfully loaded %d projects from cache in %.2f seconds",
		len(allProjects), time.Since(startTime).Seconds())

	// Get currently selected projects from database
	selectedProjects, err := db.GetSelectedProjects(userID)
	if err != nil {
		log.Printf("Error fetching selected projects: %v", err)
	}

	// Create a map for faster lookup
	selectedProjectMap := make(map[int]bool)
	for _, sp := range selectedProjects {
		selectedProjectMap[sp.ProjectID] = true
	}

	// Mark selected projects
	for i := range allProjects {
		if selectedProjectMap[allProjects[i].ID] {
			allProjects[i].Selected = true
		}
	}

	return c.Render(http.StatusOK, "settings.html", map[string]interface{}{
		"Projects": allProjects,
		"Username": session.Values["username"],
		"TreeView": false,
	})
}

// StartCacheHandler handles the request to start caching GitLab data
func StartCacheHandler(c echo.Context, store *sessions.CookieStore, gitlabURL, token string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error": "Not logged in",
		})
	}

	// Start caching in a goroutine to not block the response
	go func() {
		err := gitlab.CacheGitLabStructure(db.DB, userID, gitlabURL, token)
		if err != nil {
			log.Printf("Error caching GitLab structure: %v", err)
		}
	}()

	return c.JSON(http.StatusOK, map[string]string{
		"status":  "Caching process started in the background",
		"message": "Please wait while the GitLab structure is being cached. This may take several minutes.",
	})
}

// CacheStatusHandler handles the request to check the cache status
func CacheStatusHandler(c echo.Context) error {
	// Count cached projects and groups
	projectCount, groupCount, err := db.CountCachedItems()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": "Failed to count cached items",
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"projects": projectCount,
		"groups":   groupCount,
		"cached":   projectCount > 0 || groupCount > 0,
	})
}

// SaveSettingsHandler handles the form submission to save settings
func SaveSettingsHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Parse form
	if err := c.Request().ParseForm(); err != nil {
		return c.String(http.StatusBadRequest, "Invalid form data")
	}

	// Get selected projects from form
	selectedIDs := c.Request().Form["projects"]

	// Save to database
	err := db.SaveSelectedProjects(userID, selectedIDs)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to save settings: "+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/")
}