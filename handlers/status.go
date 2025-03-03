package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"

	"gitlab-status/db"
	"gitlab-status/gitlab"
	"gitlab-status/models"
)

// StatusPageHandler handles the status page request
func StatusPageHandler(c echo.Context, store *sessions.CookieStore, gitlabURL, token string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	// Get selected projects from database
	selectedProjects, err := db.GetSelectedProjects(userID)
	if err != nil {
		log.Printf("Error fetching selected projects: %v", err)
	}

	var statuses []models.RepositoryStatus

	// If no projects are selected yet, show a message
	if len(selectedProjects) == 0 {
		// If HTMX request, render partial with empty list
		if c.Request().Header.Get("HX-Request") != "" {
			return c.Render(http.StatusOK, "status_partial.html", statuses)
		}

		return c.Render(http.StatusOK, "status.html", map[string]interface{}{
			"Statuses":   statuses,
			"Username":   session.Values["username"],
			"NoProjects": true,
		})
	}

	for _, selectedProject := range selectedProjects {
		// Get project details from cache
		cachedProject, err := db.GetCachedProject(selectedProject.ProjectID)
		if err != nil {
			log.Printf("Error fetching project from cache for ID %d: %v", selectedProject.ProjectID, err)
			statuses = append(statuses, models.RepositoryStatus{
				RepositoryName: selectedProject.Path,
				RepositoryPath: selectedProject.Path,
				Version:        "N/A",
				PipelineID:     0,
				Status:         "Error",
				Date:           time.Time{},
			})
			continue
		}

		// Convert cached project to Project
		project := models.Project{
			ID:                cachedProject.ID,
			Name:              cachedProject.Name,
			NameWithNamespace: cachedProject.NameWithNamespace,
			Path:              cachedProject.Path,
			PathWithNamespace: cachedProject.PathWithNamespace,
			WebURL:            cachedProject.WebURL,
		}

		// Get latest pipeline
		latestPipeline, err := gitlab.FetchLatestPipeline(gitlabURL, fmt.Sprintf("%d", project.ID), token)
		if err != nil {
			log.Printf("Error fetching pipeline for %s: %v", project.PathWithNamespace, err)
			statuses = append(statuses, models.RepositoryStatus{
				RepositoryID:   project.ID,
				RepositoryName: project.Name,
				RepositoryPath: project.PathWithNamespace,
				Version:        "N/A",
				PipelineID:     0,
				Status:         "Error",
				Date:           time.Time{},
				ProjectURL:     project.WebURL,
			})
			continue
		}

		// Get recent pipelines for hover view
		recentPipelines, err := gitlab.FetchPipelines(gitlabURL, fmt.Sprintf("%d", project.ID), token, 10)
		if err != nil {
			recentPipelines = []models.Pipeline{}
		}

		// Get last successful pipeline
		lastSuccess, err := gitlab.FetchLastSuccessPipeline(gitlabURL, fmt.Sprintf("%d", project.ID), token)
		if err != nil {
			lastSuccess = nil
		}

		statuses = append(statuses, models.RepositoryStatus{
			RepositoryID:        project.ID,
			RepositoryName:      project.Name,
			RepositoryPath:      project.PathWithNamespace,
			Version:             latestPipeline.Ref,
			PipelineID:          latestPipeline.ID,
			Status:              latestPipeline.Status,
			Date:                latestPipeline.CreatedAt,
			WebURL:              latestPipeline.WebURL,
			LastSuccessPipeline: lastSuccess,
			RecentPipelines:     recentPipelines,
			ProjectURL:          project.WebURL,
		})
	}

	// If the request is an HTMX request, render the partial
	if c.Request().Header.Get("HX-Request") != "" {
		return c.Render(http.StatusOK, "status_partial.html", statuses)
	}

	return c.Render(http.StatusOK, "status.html", map[string]interface{}{
		"Statuses": statuses,
		"Username": session.Values["username"],
	})
}