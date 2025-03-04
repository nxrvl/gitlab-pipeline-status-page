package handlers

import (
	"log"
	"net/http"
	"sort"
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

// PathNode represents a node in the project path tree
type PathNode struct {
	Name      string
	Path      string
	FullPath  string
	IsProject bool
	Project   *models.CachedProject
	Children  map[string]*PathNode
	Level     int
	Expanded  bool
	Selected  bool
}

// ConvertToTemplateNode converts our internal PathNode to a template-compatible PathNode
// to avoid circular dependencies
func ConvertToTemplateNode(node *PathNode) *templates.PathNode {
	templateNode := &templates.PathNode{
		Name:      node.Name,
		Path:      node.Path,
		FullPath:  node.FullPath,
		IsProject: node.IsProject,
		Children:  make(map[string]*templates.PathNode),
		Level:     node.Level,
		Expanded:  node.Expanded,
		Selected:  node.Selected,
	}

	// Add project-specific information if it's a project
	if node.IsProject && node.Project != nil {
		templateNode.ProjectID = node.Project.ID
		templateNode.ProjectName = node.Project.Name
		templateNode.ProjectPath = node.Project.PathWithNamespace
	}

	// Convert all children recursively
	for name, child := range node.Children {
		templateNode.Children[name] = ConvertToTemplateNode(child)
	}

	return templateNode
}

// GetSortedChildKeys returns the keys of a PathNode's children sorted alphabetically
func GetSortedChildKeys(node *PathNode) []string {
	keys := make([]string, 0, len(node.Children))
	for k := range node.Children {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// CountProjects returns the total number of projects in a node and all its children
func CountProjects(node *PathNode) int {
	count := 0

	// If this is a project, count it
	if node.IsProject {
		return 1
	}

	// Count all projects in child nodes
	for _, child := range node.Children {
		count += CountProjects(child)
	}

	return count
}

// BuildPathIndicator creates a graphical path indicator (tree lines)
// for visual display of the hierarchy
func BuildPathIndicator(level int) string {
	if level <= 1 {
		return ""
	}

	// Create a string of vertical and horizontal lines
	// Each level gets a vertical bar, last level gets horizontal connector
	var indicator strings.Builder

	// Add vertical bars for each level
	for i := 1; i < level; i++ {
		indicator.WriteString("│ ")
	}

	// Replace last character with a connector
	indicatorStr := indicator.String()
	if len(indicatorStr) >= 2 {
		indicatorStr = indicatorStr[:len(indicatorStr)-2] + "├─"
	}

	return indicatorStr
}

// FilterProjects returns a filtered list of cached projects that match the search term
func FilterProjects(projects []models.CachedProject, searchTerm string) []models.CachedProject {
	if searchTerm == "" {
		return projects
	}

	searchTerm = strings.ToLower(searchTerm)
	var filtered []models.CachedProject

	for _, project := range projects {
		// Check if search term is found in project name or path
		if strings.Contains(strings.ToLower(project.Name), searchTerm) ||
			strings.Contains(strings.ToLower(project.PathWithNamespace), searchTerm) {
			filtered = append(filtered, project)
		}
	}

	return filtered
}

// IsPathInSearch checks if any part of the path matches the search term
func IsPathInSearch(path string, searchTerm string) bool {
	if searchTerm == "" {
		return true
	}

	searchTerm = strings.ToLower(searchTerm)
	return strings.Contains(strings.ToLower(path), searchTerm)
}

// EnsurePathVisibility makes sure all parent groups of matching items are expanded
func EnsurePathVisibility(node *PathNode, searchTerm string) bool {
	// If this is a search and the node itself doesn't match, check children
	if searchTerm != "" && !IsPathInSearch(node.FullPath, searchTerm) {
		// Check if any child matches
		hasMatchingChild := false

		for _, child := range node.Children {
			if EnsurePathVisibility(child, searchTerm) {
				hasMatchingChild = true
			}
		}

		// If any child matches, expand this node
		if hasMatchingChild {
			node.Expanded = true
		}

		return hasMatchingChild
	}

	// If this node matches or no search term, it's visible
	// and we also need to expand it if it's a group
	if !node.IsProject {
		node.Expanded = true
	}

	return true
}

// storeExpandedState stores the expanded state of a node in a map for persistence across requests
func storeExpandedState(node *PathNode, expandedPaths map[string]bool) {
	if !node.IsProject && node.Expanded {
		expandedPaths[node.FullPath] = true
	} else if !node.IsProject && !node.Expanded {
		// If explicitly collapsed, ensure it's marked as such
		expandedPaths[node.FullPath] = false
	}

	for _, child := range node.Children {
		storeExpandedState(child, expandedPaths)
	}
}

// applyExpandedState applies previously saved expanded state to a tree
func applyExpandedState(node *PathNode, expandedPaths map[string]bool) {
	if !node.IsProject {
		if expanded, exists := expandedPaths[node.FullPath]; exists {
			node.Expanded = expanded
		}
	}

	for _, child := range node.Children {
		applyExpandedState(child, expandedPaths)
	}
}

// SettingsPageHandler handles the settings page request with path-based tree view
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
	path := c.QueryParam("path")
	selectState := c.QueryParam("select")

	// Get expanded paths from session
	var expandedPaths map[string]bool
	expandedPathsInterface, exists := session.Values["expanded_paths"]
	if !exists {
		expandedPaths = make(map[string]bool)
	} else {
		expandedPaths = expandedPathsInterface.(map[string]bool)
	}

	// Check if we have cached data
	projectCount, _, err := db.CountCachedItems()
	if err != nil {
		log.Printf("Error checking cached items: %v", err)
		return templates.Settings(
			session.Values["username"].(string),
			true,
			false,
			"Failed to check database cache: "+err.Error(),
			gitlabURL,
			nil,
			nil,
			"",
		).Render(c.Request().Context(), c.Response().Writer)
	}

	// If we don't have cached data, show caching message
	if projectCount == 0 {
		log.Printf("No cached projects found in database")
		return templates.Settings(
			session.Values["username"].(string),
			true,
			true,
			"No projects found in database. Click Refresh Data to load GitLab projects.",
			gitlabURL,
			nil,
			nil,
			"",
		).Render(c.Request().Context(), c.Response().Writer)
	}

	// Load all cached projects
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

	// Get currently selected projects from database
	selectedProjects, _ := db.GetSelectedProjects(userID)
	selectedProjectMap := make(map[int]bool)
	for _, sp := range selectedProjects {
		selectedProjectMap[sp.ProjectID] = true
	}

	// Build path-based tree structure with search filter
	rootNode := buildProjectPathTree(cachedProjects, selectedProjectMap, searchTerm)

	// Apply previously saved expanded state to the tree
	applyExpandedState(rootNode, expandedPaths)

	// If this is an expand/collapse action, update the tree
	if (action == "expand" || action == "collapse") && path != "" {
		updateNodeExpandState(rootNode, path, action == "expand", expandedPaths)

		// Save expanded paths to session
		session.Values["expanded_paths"] = expandedPaths
		session.Save(c.Request(), c.Response())

		// If it's an HTMX request, return only the updated tree
		if c.Request().Header.Get("HX-Request") == "true" {
			// Convert internal node to template-compatible node
			templateNode := ConvertToTemplateNode(rootNode)
			return templates.RenderPathTree(templateNode).Render(c.Request().Context(), c.Response().Writer)
		}
	}

	// Handle selection action
	if action == "select" && path != "" {
		isSelected := selectState == "true"
		processNodeSelection(rootNode, path, isSelected)

		// If it's an HTMX request, return only the updated tree
		if c.Request().Header.Get("HX-Request") == "true" {
			// Convert internal node to template-compatible node
			templateNode := ConvertToTemplateNode(rootNode)
			return templates.RenderPathTree(templateNode).Render(c.Request().Context(), c.Response().Writer)
		}
	}

	// For HTMX search requests, only return the tree
	if searchTerm != "" && c.Request().Header.Get("HX-Request") == "true" {
		// If searching, ensure all paths to matching nodes are expanded
		EnsurePathVisibility(rootNode, searchTerm)

		// Store new expanded state in map
		storeExpandedState(rootNode, expandedPaths)

		// Save expanded paths to session
		session.Values["expanded_paths"] = expandedPaths
		session.Save(c.Request(), c.Response())

		// Convert internal node to template-compatible node
		templateNode := ConvertToTemplateNode(rootNode)
		return templates.RenderPathTree(templateNode).Render(c.Request().Context(), c.Response().Writer)
	}

	// Convert path tree to group tree for template
	groupTree := convertPathNodeToGroupTree(rootNode)

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

// buildProjectPathTree builds a tree structure from projects' path_with_namespace
func buildProjectPathTree(projects []models.CachedProject, selectedProjectMap map[int]bool, searchTerm string) *PathNode {
	root := &PathNode{
		Name:      "Root",
		Path:      "",
		FullPath:  "",
		IsProject: false,
		Children:  make(map[string]*PathNode),
		Level:     0,
		Expanded:  true,
	}

	// Filter projects by search term if needed
	filteredProjects := FilterProjects(projects, searchTerm)

	for _, project := range filteredProjects {
		// Split the path_with_namespace into parts
		parts := strings.Split(project.PathWithNamespace, "/")
		current := root
		fullPath := ""

		for i, part := range parts {
			if i > 0 {
				fullPath = fullPath + "/" + part
			} else {
				fullPath = part
			}

			// If this is the last part, it's a project, otherwise it's a directory/group
			isProject := i == len(parts)-1

			if isProject {
				// Create a leaf node for the project
				projectNode := &PathNode{
					Name:      part,
					Path:      part,
					FullPath:  fullPath,
					IsProject: true,
					Project:   &project,
					Children:  nil,
					Level:     i + 1,
					Expanded:  false, // Projects don't have children
					Selected:  selectedProjectMap[project.ID],
				}
				current.Children[part] = projectNode
			} else {
				// Create or get the directory/group node
				if _, exists := current.Children[part]; !exists {
					current.Children[part] = &PathNode{
						Name:      part,
						Path:      part,
						FullPath:  fullPath,
						IsProject: false,
						Children:  make(map[string]*PathNode),
						Level:     i + 1,
						Expanded:  i < 1, // Expand only top-level by default
					}
				}
				current = current.Children[part]
			}
		}
	}

	// Update selection state of parent nodes based on children
	updateParentSelectionState(root)

	// If searching, ensure all paths to matching nodes are expanded
	if searchTerm != "" {
		EnsurePathVisibility(root, searchTerm)
	}

	return root
}

// updateParentSelectionState recursively updates parent selection state based on children
func updateParentSelectionState(node *PathNode) bool {
	if node.IsProject {
		return node.Selected
	}

	if len(node.Children) == 0 {
		return false
	}

	// Check if all children are selected
	allSelected := true
	for _, child := range node.Children {
		childSelected := updateParentSelectionState(child)
		if !childSelected {
			allSelected = false
		}
	}

	// A node is selected if all its children are selected
	node.Selected = allSelected && len(node.Children) > 0

	return node.Selected
}

// updateNodeExpandState recursively finds a node and updates its expanded state
func updateNodeExpandState(node *PathNode, targetPath string, expanded bool, expandedPaths map[string]bool) bool {
	if node.FullPath == targetPath {
		node.Expanded = expanded
		expandedPaths[targetPath] = expanded
		return true
	}

	for _, child := range node.Children {
		if !child.IsProject && updateNodeExpandState(child, targetPath, expanded, expandedPaths) {
			return true
		}
	}

	return false
}

// processNodeSelection handles selection/deselection of a node and its children
func processNodeSelection(node *PathNode, targetPath string, selected bool) bool {
	if node.FullPath == targetPath {
		// Set this node's selection
		node.Selected = selected

		// Recursively propagate to all children
		selectNodeAndChildren(node, selected)
		return true
	}

	for _, child := range node.Children {
		if !child.IsProject && processNodeSelection(child, targetPath, selected) {
			// Update parent nodes' selection state after changing children
			updateParentSelectionState(node)
			return true
		}
	}

	return false
}

// selectNodeAndChildren selects or deselects a node and all its children
func selectNodeAndChildren(node *PathNode, selected bool) {
	node.Selected = selected

	// Process children recursively
	for _, child := range node.Children {
		selectNodeAndChildren(child, selected)
	}
}

// convertPathNodeToGroupTree converts a PathNode tree to []models.Group for template compatibility
func convertPathNodeToGroupTree(node *PathNode) []models.Group {
	var result []models.Group

	// Skip the root node itself
	for name, child := range node.Children {
		// Only process non-project nodes as groups
		if !child.IsProject {
			group := convertNodeToGroup(name, child)
			result = append(result, group)
		}
	}

	return result
}

// convertNodeToGroup converts a PathNode to a models.Group with its projects and subgroups
func convertNodeToGroup(name string, node *PathNode) models.Group {
	group := models.Group{
		ID:          0, // We don't have actual GitLab group IDs from path structure
		Name:        name,
		Path:        node.Path,
		FullPath:    node.FullPath,
		WebURL:      "", // We don't have actual URLs from path structure
		Subgroups:   []models.Group{},
		Projects:    []models.Project{},
		Level:       node.Level - 1, // Adjust level to match existing template expectations
		HasChildren: len(node.Children) > 0,
		Expanded:    node.Expanded,
		Selected:    node.Selected,
	}

	// Get sorted child keys for consistent ordering
	childKeys := GetSortedChildKeys(node)

	// Process children in sorted order
	for _, childName := range childKeys {
		childNode := node.Children[childName]
		if childNode.IsProject {
			// Add as project
			project := models.Project{
				ID:                childNode.Project.ID,
				Name:              childNode.Project.Name,
				NameWithNamespace: childNode.Project.NameWithNamespace,
				Path:              childNode.Project.Path,
				PathWithNamespace: childNode.Project.PathWithNamespace,
				WebURL:            childNode.Project.WebURL,
				Level:             childNode.Level - 1, // Adjust level
				Selected:          childNode.Selected,
			}
			group.Projects = append(group.Projects, project)
		} else {
			// Add as subgroup
			subgroup := convertNodeToGroup(childName, childNode)
			group.Subgroups = append(group.Subgroups, subgroup)
		}
	}

	return group
}

// ProjectsPageHandler handles the projects page request (flat list of all projects)
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

// RenderPathTreeHandler handles HTMX requests to render just the path tree component
func RenderPathTreeHandler(c echo.Context, store *sessions.CookieStore, gitlabURL string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.String(http.StatusUnauthorized, "Unauthorized")
	}

	// Get search term
	searchTerm := c.QueryParam("search")

	// Check for action parameter (expand/collapse/select)
	action := c.QueryParam("action")
	path := c.QueryParam("path")
	selectState := c.QueryParam("select")

	// Get expanded paths from session
	var expandedPaths map[string]bool
	expandedPathsInterface, exists := session.Values["expanded_paths"]
	if !exists {
		expandedPaths = make(map[string]bool)
	} else {
		expandedPaths = expandedPathsInterface.(map[string]bool)
	}

	// Load all cached projects
	cachedProjects, err := db.GetCachedProjects()
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to load projects from database")
	}

	// Get currently selected projects from database
	selectedProjects, _ := db.GetSelectedProjects(userID)
	selectedProjectMap := make(map[int]bool)
	for _, sp := range selectedProjects {
		selectedProjectMap[sp.ProjectID] = true
	}

	// Build path-based tree structure with search filter
	rootNode := buildProjectPathTree(cachedProjects, selectedProjectMap, searchTerm)

	// Apply previously saved expanded state to the tree
	applyExpandedState(rootNode, expandedPaths)

	// If this is an expand/collapse action, update the tree
	if (action == "expand" || action == "collapse") && path != "" {
		updateNodeExpandState(rootNode, path, action == "expand", expandedPaths)

		// Save expanded paths to session
		session.Values["expanded_paths"] = expandedPaths
		session.Save(c.Request(), c.Response())
	}

	// Handle selection action
	if action == "select" && path != "" {
		isSelected := selectState == "true"
		processNodeSelection(rootNode, path, isSelected)
	}

	// If searching, ensure all paths to matching nodes are expanded
	if searchTerm != "" {
		EnsurePathVisibility(rootNode, searchTerm)

		// Store new expanded state in map
		storeExpandedState(rootNode, expandedPaths)

		// Save expanded paths to session
		session.Values["expanded_paths"] = expandedPaths
		session.Save(c.Request(), c.Response())
	}

	// Convert internal node to template-compatible node
	templateNode := ConvertToTemplateNode(rootNode)

	// Return only the tree component
	return templates.RenderPathTree(templateNode).Render(c.Request().Context(), c.Response().Writer)
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

// For compatibility with the SaveSettingsHandler, collect all selected project IDs
func collectSelectedProjectIDs(node *PathNode) []string {
	var result []string

	if node.IsProject && node.Selected {
		result = append(result, strconv.Itoa(node.Project.ID))
	}

	for _, child := range node.Children {
		childIDs := collectSelectedProjectIDs(child)
		result = append(result, childIDs...)
	}

	return result
}
