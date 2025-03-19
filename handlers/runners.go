package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"

	"gitlab-status/db"
	"gitlab-status/gitlab"
	"gitlab-status/models"
	"gitlab-status/templates"
)

// RunnersPageHandler handles the runners page request
func RunnersPageHandler(c echo.Context, store *sessions.CookieStore, gitlabURL string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")

	// Get user ID from session
	userID, ok := session.Values["user_id"].(int64)
	if !ok {
		return c.Redirect(http.StatusSeeOther, "/logout")
	}

	username := session.Values["username"].(string)

	// Check if we have cached projects
	projectCount, _, err := db.CountCachedItems()
	if err != nil {
		log.Printf("Error checking cached projects: %v", err)
	}

	// If we don't have cached projects, show message to refresh
	if projectCount == 0 {
		log.Printf("No cached projects found in database")
		return templates.Runners(
			username,
			true,
			"No projects found in database. Click Refresh Data to load GitLab projects and runners.",
			gitlabURL,
			nil,
		).Render(c.Request().Context(), c.Response().Writer)
	}

	// Get projects with runners from database
	projectsWithRunners, err := db.GetProjectsWithRunners(userID)
	if err != nil {
		log.Printf("Error fetching projects with runners: %v", err)
		return templates.Runners(
			username,
			false,
			"Failed to load projects with runners: "+err.Error(),
			gitlabURL,
			nil,
		).Render(c.Request().Context(), c.Response().Writer)
	}

	if len(projectsWithRunners) == 0 {
		// No projects with runners found, but we have projects in the database
		return templates.Runners(
			username,
			false,
			"No projects with runners found. Click Refresh Runners to fetch runner information.",
			gitlabURL,
			nil,
		).Render(c.Request().Context(), c.Response().Writer)
	}

	// Render the runners page
	return templates.Runners(
		username,
		false,
		"",
		gitlabURL,
		projectsWithRunners,
	).Render(c.Request().Context(), c.Response().Writer)
}

// RefreshRunnersHandler handles the request to refresh runner data
func RefreshRunnersHandler(c echo.Context, store *sessions.CookieStore, gitlabURL, token string) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	username := session.Values["username"].(string)

	// Start refreshing in background
	go func() {
		startTime := time.Now()
		log.Printf("Starting to refresh runner data...")

		// Get all runners (summary information)
		runners, err := gitlab.FetchAllRunners(gitlabURL, token)
		if err != nil {
			log.Printf("Error fetching runners: %v", err)
			return
		}

		// Get detailed information for each runner
		detailedRunners := make([]models.Runner, 0, len(runners))
		runnerProjects := make(map[int][]models.Project)

		for _, runnerSummary := range runners {
			runner, err := gitlab.FetchRunner(gitlabURL, token, runnerSummary.ID)
			if err != nil {
				log.Printf("Warning: Failed to fetch details for runner %d: %v", runnerSummary.ID, err)
				// Still include the summary runner if we can't get details
				detailedRunners = append(detailedRunners, runnerSummary)
				continue
			}

			// Add the detailed runner to our collection
			detailedRunners = append(detailedRunners, *runner)

			// Store projects in the map (for backward compatibility)
			if runner.Projects != nil && len(runner.Projects) > 0 {
				runnerProjects[runner.ID] = runner.Projects
			}
		}

		// Store runner data in database
		err = db.CacheRunnerData(detailedRunners, runnerProjects)
		if err != nil {
			log.Printf("Error caching runner data: %v", err)
			return
		}

		log.Printf("Successfully refreshed data for %d runners in %.2f seconds",
			len(detailedRunners), time.Since(startTime).Seconds())
	}()

	// Show loading message
	return templates.Runners(
		username,
		true,
		"Refreshing runner data. Please wait and refresh the page in a few moments.",
		gitlabURL,
		nil,
	).Render(c.Request().Context(), c.Response().Writer)
}

// SaveRunnersSelectionHandler handles saving which projects with runners to monitor
func SaveRunnersSelectionHandler(c echo.Context, store *sessions.CookieStore) error {
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
	selectedIDs := c.Request().Form["runner_projects"]

	// Save to database
	err := db.SaveSelectedProjects(userID, selectedIDs)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to save settings: "+err.Error())
	}

	// If it's an HTMX request, return success message
	if c.Request().Header.Get("HX-Request") == "true" {
		return c.HTML(http.StatusOK, "<div class='alert alert-success'>Settings saved successfully!</div>")
	}

	return c.Redirect(http.StatusSeeOther, "/runners")
}
