package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"

	"gitlab-status/db"
	"gitlab-status/gitlab"
	"gitlab-status/models"
	"gitlab-status/templates"
)

// SettingsPageHandler handles the settings page request with templ
func SettingsPageHandler(c echo.Context, store *sessions.CookieStore, gitlabURL string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Get search term
	searchTerm := c.QueryParam("search")

	// Check for action parameter (expand/collapse/select)
	action := c.QueryParam("action")
	groupIDStr := c.QueryParam("groupID")
	selectState := c.QueryParam("select")

	// Load all cached groups and projects
	cachedGroups, err := db.GetCachedGroups()
	if err != nil {
		log.Printf("Error loading groups from cache: %v", err)
		return templates.Settings(
			session.Values["username"].(string),
			true,
			false,
			"Failed to load groups from cache: "+err.Error(),
			gitlabURL,
			nil,
			nil,
			"",
		).Render(c.Request().Context(), c.Response().Writer)
	}

	cachedProjects, err := db.GetCachedProjects()
	if err != nil {
		log.Printf("Error loading projects from cache: %v", err)
		return templates.Settings(
			session.Values["username"].(string),
			true,
			false,
			"Failed to load projects from cache: "+err.Error(),
			gitlabURL,
			nil,
			nil,
			"",
		).Render(c.Request().Context(), c.Response().Writer)
	}

	// Build path-based tree structure with search filter
	groupTree := buildNestedGroupTree(cachedGroups, cachedProjects, searchTerm)

	// If this is an expand/collapse action, update the tree
	if (action == "expand" || action == "collapse") && groupIDStr != "" {
		groupID, err := strconv.Atoi(groupIDStr)
		if err == nil {
			updateGroupExpandState(groupTree, groupID, action == "expand")
		}
	}

	// Handle group selection action
	if action == "select" && groupIDStr != "" {
		groupID, err := strconv.Atoi(groupIDStr)
		if err == nil {
			isSelected := selectState == "true"
			processGroupSelection(groupTree, groupID, isSelected)

			// If it's an HTMX request, return only the updated tree
			if c.Request().Header.Get("HX-Request") == "true" {
				return templates.RenderGroups(groupTree).Render(c.Request().Context(), c.Response().Writer)
			}
		}
	}

	// Get currently selected projects from database
	selectedProjects, _ := db.GetSelectedProjects(userID)
	selectedProjectMap := make(map[int]bool)
	for _, sp := range selectedProjects {
		selectedProjectMap[sp.ProjectID] = true
	}

	// Mark selected projects in the tree
	markSelectedProjectsAndGroups(groupTree, selectedProjectMap)

	// For HTMX search requests, only return the tree
	if searchTerm != "" && c.Request().Header.Get("HX-Request") == "true" {
		return templates.RenderGroups(groupTree).Render(c.Request().Context(), c.Response().Writer)
	}

	return templates.Settings(
		session.Values["username"].(string),
		true,
		false,
		"",
		gitlabURL,
		groupTree,
		nil,
		searchTerm,
	).Render(c.Request().Context(), c.Response().Writer)
}

func processGroupSelection(groups []models.Group, targetID int, selected bool) bool {
	for i := range groups {
		if groups[i].ID == targetID {
			// Set this group's selection
			groups[i].Selected = selected

			// Propagate to all projects
			for j := range groups[i].Projects {
				groups[i].Projects[j].Selected = selected
			}

			// Recursively propagate to subgroups
			for j := range groups[i].Subgroups {
				selectGroupAndChildren(&groups[i].Subgroups[j], selected)
			}

			return true
		}

		// Check subgroups recursively
		if processGroupSelection(groups[i].Subgroups, targetID, selected) {
			return true
		}
	}

	return false
}

// Helper to select/deselect a group and all its children
func selectGroupAndChildren(group *models.Group, selected bool) {
	group.Selected = selected

	// Select/deselect all projects
	for i := range group.Projects {
		group.Projects[i].Selected = selected
	}

	// Recursively select/deselect all subgroups
	for i := range group.Subgroups {
		selectGroupAndChildren(&group.Subgroups[i], selected)
	}
}

// ProjectsPageHandler handles the projects page request
func ProjectsPageHandler(c echo.Context, store *sessions.CookieStore, gitlabURL string) error {
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
		return templates.Settings(
			session.Values["username"].(string),
			false,
			true,
			"No projects found in database. Click Refresh Data to load GitLab projects.",
			gitlabURL,
			nil,
			nil,
			"",
		).Render(c.Request().Context(), c.Response().Writer)
	}

	// Get projects from cache
	log.Printf("Loading projects from cache")
	startTime := time.Now()

	// Load projects from cache
	cachedProjects, err := db.GetCachedProjects()
	if err != nil {
		log.Printf("Error loading projects from cache: %v", err)
		return templates.Settings(
			session.Values["username"].(string),
			false,
			false,
			"Failed to load projects from cache: "+err.Error(),
			gitlabURL,
			nil,
			nil,
			"",
		).Render(c.Request().Context(), c.Response().Writer)
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

	return templates.Settings(
		session.Values["username"].(string),
		false,
		false,
		"",
		gitlabURL,
		nil,
		allProjects,
		"",
	).Render(c.Request().Context(), c.Response().Writer)
}

// CacheHandler handles direct navigation to cache refresh
func CacheHandler(c echo.Context, store *sessions.CookieStore, gitlabURL, token string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	_, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Start caching in a goroutine to not block the response
	go func() {
		// Fetch groups and projects
		groups, err := gitlab.FetchGroups(gitlabURL, token)
		if err != nil {
			log.Printf("Error fetching groups: %v", err)
			return
		}

		projects, err := gitlab.FetchProjects(gitlabURL, token)
		if err != nil {
			log.Printf("Error fetching projects: %v", err)
			return
		}

		// Store in database
		err = db.CacheGitLabStructure(groups, projects)
		if err != nil {
			log.Printf("Error caching GitLab structure: %v", err)
		}
	}()

	// Redirect to settings page with caching message
	return templates.Settings(
		session.Values["username"].(string),
		true,
		true,
		"Refreshing GitLab data. Please wait and refresh the page in a few moments.",
		gitlabURL,
		nil,
		nil,
		"",
	).Render(c.Request().Context(), c.Response().Writer)
}

// SaveSettingsHandler handles the form submission to save settings
func SaveSettingsHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")
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

	// If it's an HTMX request, return success message
	if c.Request().Header.Get("HX-Request") == "true" {
		return c.HTML(http.StatusOK, "<div class='alert alert-success'>Settings saved successfully!</div>")
	}

	return c.Redirect(http.StatusSeeOther, "/")
}

// buildNestedGroupTree builds a proper nested group tree based on full path
func buildNestedGroupTree(cachedGroups []models.CachedGroup, cachedProjects []models.CachedProject, searchTerm string) []models.Group {
	// Create maps for quick lookup
	groupByID := make(map[int]models.Group)
	groupByPath := make(map[string]models.Group)

	// First create all groups
	for _, cg := range cachedGroups {
		// Apply search filter if provided
		if searchTerm != "" && !strings.Contains(strings.ToLower(cg.Name+cg.FullPath), strings.ToLower(searchTerm)) {
			continue
		}

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
			Expanded:    true,  // Default expanded for top-level
			Selected:    false, // Add selection state for groups
		}

		groupByID[group.ID] = group
		groupByPath[group.FullPath] = group
	}

	// Create a map of projects by their group's full path
	projectsByPath := make(map[string][]models.Project)

	// Process all projects
	for _, cp := range cachedProjects {
		// Apply search filter
		if searchTerm != "" && !strings.Contains(strings.ToLower(cp.Name+cp.PathWithNamespace), strings.ToLower(searchTerm)) {
			continue
		}

		project := models.Project{
			ID:                cp.ID,
			Name:              cp.Name,
			NameWithNamespace: cp.NameWithNamespace,
			Path:              cp.Path,
			PathWithNamespace: cp.PathWithNamespace,
			WebURL:            cp.WebURL,
			Level:             0, // Will be set correctly later
			Selected:          false,
		}

		// Set namespace info
		project.Namespace.ID = cp.GroupID
		project.Namespace.Path = cp.Path

		// Get group path from project path
		parts := strings.Split(cp.PathWithNamespace, "/")
		if len(parts) > 1 {
			// Get group path by removing the last part (project name)
			groupPath := strings.Join(parts[:len(parts)-1], "/")
			projectsByPath[groupPath] = append(projectsByPath[groupPath], project)
		} else {
			// Handle projects in root (if any)
			projectsByPath[""] = append(projectsByPath[""], project)
		}
	}

	// Build the actual tree structure
	var rootGroups []models.Group

	// Process groups to build the hierarchy
	for _, cg := range cachedGroups {
		if _, exists := groupByID[cg.ID]; !exists {
			continue // Skip if filtered out by search
		}

		group := groupByID[cg.ID]

		// Add projects to this group
		if projects, exists := projectsByPath[cg.FullPath]; exists {
			group.Projects = projects
			group.HasChildren = true
		}

		// If it's a top-level group, add to root groups
		if !strings.Contains(cg.FullPath, "/") {
			rootGroups = append(rootGroups, group)
			continue
		}

		// Otherwise, find its parent and add it as a subgroup
		lastSlashIndex := strings.LastIndex(cg.FullPath, "/")
		if lastSlashIndex > 0 {
			parentPath := cg.FullPath[:lastSlashIndex]
			if parent, exists := groupByPath[parentPath]; exists {
				parent.Subgroups = append(parent.Subgroups, group)
				parent.HasChildren = true
				groupByPath[parentPath] = parent
			}
		}
	}

	// Set levels and update the groups recursively
	setGroupLevels(rootGroups, 0)

	// Update the root groups list with the modified ones
	for i, group := range rootGroups {
		if updatedGroup, exists := groupByPath[group.FullPath]; exists {
			rootGroups[i] = updatedGroup
		}
	}

	return rootGroups
}

// markSelectedProjectsAndGroups recursively marks selected projects and groups
func markSelectedProjectsAndGroups(groups []models.Group, selectedProjectMap map[int]bool) {
	for i := range groups {
		// Mark projects as selected based on the map
		allProjectsSelected := len(groups[i].Projects) > 0
		for j := range groups[i].Projects {
			if selectedProjectMap[groups[i].Projects[j].ID] {
				groups[i].Projects[j].Selected = true
			} else {
				allProjectsSelected = false
			}
		}

		// Process subgroups recursively
		allSubgroupsSelected := true
		if len(groups[i].Subgroups) > 0 {
			markSelectedProjectsAndGroups(groups[i].Subgroups, selectedProjectMap)

			// Check if all subgroups are selected
			for _, subgroup := range groups[i].Subgroups {
				if !subgroup.Selected {
					allSubgroupsSelected = false
					break
				}
			}
		}

		// A group is selected if all its projects and all its subgroups are selected
		groups[i].Selected = allProjectsSelected && allSubgroupsSelected &&
			(len(groups[i].Projects) > 0 || len(groups[i].Subgroups) > 0)
	}
}

// setGroupLevels recursively sets the correct level for each group and its children
func setGroupLevels(groups []models.Group, level int) {
	for i := range groups {
		groups[i].Level = level

		// Set level for projects
		for j := range groups[i].Projects {
			groups[i].Projects[j].Level = level + 1
		}

		// Recursively set level for subgroups
		setGroupLevels(groups[i].Subgroups, level+1)
	}
}

// updateGroupExpandState recursively updates the expanded state of a group
func updateGroupExpandState(groups []models.Group, targetID int, expanded bool) bool {
	for i := range groups {
		if groups[i].ID == targetID {
			groups[i].Expanded = expanded
			return true
		}

		// Check subgroups recursively
		if updateGroupExpandState(groups[i].Subgroups, targetID, expanded) {
			return true
		}
	}

	return false
}

func RenderGroupsHandler(c echo.Context, store *sessions.CookieStore, gitlabURL string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	// Get search term
	searchTerm := c.QueryParam("search")

	// Load all cached groups and projects
	cachedGroups, _ := db.GetCachedGroups()
	cachedProjects, _ := db.GetCachedProjects()

	// Build path-based tree structure with search filter
	groupTree := buildNestedGroupTree(cachedGroups, cachedProjects, searchTerm)

	// Get currently selected projects from database
	selectedProjects, _ := db.GetSelectedProjects(userID)
	selectedProjectMap := make(map[int]bool)
	for _, sp := range selectedProjects {
		selectedProjectMap[sp.ProjectID] = true
	}

	// Mark selected projects in the tree
	markSelectedProjectsAndGroups(groupTree, selectedProjectMap)

	// Return only the tree component
	return templates.RenderGroups(groupTree).Render(c.Request().Context(), c.Response().Writer)
}
