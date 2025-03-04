package handlers

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"

	"gitlab-status/db"
	"gitlab-status/models"
)

// ProjectsMdStructureHandler generates a markdown structure based solely on the
// path_with_namespace column of cached projects
func ProjectsMdStructureHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	_, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Load all cached projects
	var cachedProjects []models.CachedProject
	err := db.DB.NewSelect().Model(&cachedProjects).Order("path_with_namespace ASC").Scan(context.Background())
	if err != nil {
		log.Printf("Error loading projects from cache: %v", err)
		return c.String(http.StatusInternalServerError, "Failed to load projects from database")
	}

	// Create a tree structure based on path_with_namespace
	pathTree := buildProjectPathTree(cachedProjects)

	// Generate markdown content
	var buffer bytes.Buffer
	buffer.WriteString("# GitLab Projects Structure\n\n")
	buffer.WriteString(fmt.Sprintf("Generated on: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	writePathTreeToMarkdown(&buffer, pathTree, 0)

	// Set response headers for file download
	c.Response().Header().Set("Content-Disposition", "attachment; filename=gitlab-projects-structure.md")
	c.Response().Header().Set("Content-Type", "text/markdown")

	return c.String(http.StatusOK, buffer.String())
}

// PathNode represents a node in the path tree
type PathNode struct {
	Name     string
	Path     string
	FullPath string
	IsGroup  bool
	Projects []models.CachedProject
	Children map[string]*PathNode
}

// buildProjectPathTree builds a tree structure from projects' path_with_namespace
func buildProjectPathTree(projects []models.CachedProject) *PathNode {
	root := &PathNode{
		Name:     "Root",
		Path:     "",
		FullPath: "",
		IsGroup:  true,
		Children: make(map[string]*PathNode),
	}

	for _, project := range projects {
		// Split the path_with_namespace into parts
		parts := strings.Split(project.PathWithNamespace, "/")

		// Navigate the tree, creating nodes as needed
		current := root
		fullPath := ""

		for i, part := range parts {
			if i > 0 {
				fullPath = fullPath + "/" + part
			} else {
				fullPath = part
			}

			// If this is the last part, it's a project, otherwise it's a group
			isProject := i == len(parts)-1

			if isProject {
				// Add the project to the current node's projects
				current.Projects = append(current.Projects, project)
			} else {
				// Create or get the group node
				if _, exists := current.Children[part]; !exists {
					current.Children[part] = &PathNode{
						Name:     part,
						Path:     part,
						FullPath: fullPath,
						IsGroup:  true,
						Children: make(map[string]*PathNode),
					}
				}
				current = current.Children[part]
			}
		}
	}

	return root
}

// writePathTreeToMarkdown recursively writes the path tree to markdown
func writePathTreeToMarkdown(buffer *bytes.Buffer, node *PathNode, level int) {
	// Skip writing the root node
	if level > 0 {
		prefix := strings.Repeat("#", level+1)
		buffer.WriteString(fmt.Sprintf("%s %s\n\n", prefix, node.Name))

		if node.FullPath != "" {
			buffer.WriteString(fmt.Sprintf("- **Full Path:** `%s`\n\n", node.FullPath))
		}
	}

	// Write projects in this node
	if len(node.Projects) > 0 {
		if level > 0 {
			buffer.WriteString("**Projects:**\n\n")
		}

		for _, project := range node.Projects {
			buffer.WriteString(fmt.Sprintf("- [%s](%s): `%s`\n",
				project.Name,
				project.WebURL,
				project.PathWithNamespace))
		}
		buffer.WriteString("\n")
	}

	// Sort children by name for consistent output
	var childrenNames []string
	for name := range node.Children {
		childrenNames = append(childrenNames, name)
	}
	sort.Strings(childrenNames)

	// Recursively write children
	for _, name := range childrenNames {
		writePathTreeToMarkdown(buffer, node.Children[name], level+1)
	}
}

func DownloadStructureHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	_, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Load all cached groups and projects
	cachedGroups, err := db.GetCachedGroups()
	if err != nil {
		log.Printf("Error loading groups: %v", err)
		return c.String(http.StatusInternalServerError, "Failed to load groups from database")
	}

	cachedProjects, err := db.GetCachedProjects()
	if err != nil {
		log.Printf("Error loading projects: %v", err)
		return c.String(http.StatusInternalServerError, "Failed to load projects from database")
	}

	// Build path-based tree structure
	groupTree := buildNestedGroupTree(cachedGroups, cachedProjects, "")

	// Generate markdown content
	var buffer bytes.Buffer
	buffer.WriteString("# GitLab Structure\n\n")
	buffer.WriteString(fmt.Sprintf("Generated on: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	writeGroupsToMarkdown(&buffer, groupTree, 0)

	// Set response headers for file download
	c.Response().Header().Set("Content-Disposition", "attachment; filename=gitlab-structure.md")
	c.Response().Header().Set("Content-Type", "text/markdown")

	return c.String(http.StatusOK, buffer.String())
}

// writeGroupsToMarkdown writes the group structure and projects to the markdown buffer
func writeGroupsToMarkdown(buffer *bytes.Buffer, groups []models.Group, level int) {
	for _, group := range groups {
		// Write group header with appropriate heading level (## for top level, ### for second level, etc.)
		prefix := strings.Repeat("#", level+2)
		buffer.WriteString(fmt.Sprintf("%s %s\n\n", prefix, group.Name))

		// Add group details
		buffer.WriteString(fmt.Sprintf("- **Path:** %s\n", group.FullPath))
		buffer.WriteString(fmt.Sprintf("- **URL:** %s\n\n", group.WebURL))

		// Add projects in this group
		if len(group.Projects) > 0 {
			buffer.WriteString("**Projects:**\n\n")
			for _, project := range group.Projects {
				buffer.WriteString(fmt.Sprintf("- [%s](%s): `%s`\n",
					project.Name,
					project.WebURL,
					project.PathWithNamespace))
			}
			buffer.WriteString("\n")
		}

		// Recursively add subgroups
		if len(group.Subgroups) > 0 {
			writeGroupsToMarkdown(buffer, group.Subgroups, level+1)
		}
	}
}
