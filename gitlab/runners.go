package gitlab

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gitlab-status/models"
)

// FetchAllRunners gets all GitLab runners accessible with the token
func FetchAllRunners(gitlabURL, token string) ([]models.Runner, error) {
	page := 1
	perPage := 100 // Maximum allowed by GitLab API
	maxPages := 5  // Limit number of pages to avoid extremely long requests
	allRunners := []models.Runner{}

	log.Printf("Fetching GitLab runners from %s", gitlabURL)

	for page <= maxPages {
		apiURL := fmt.Sprintf("%s/api/v4/runners/all?per_page=%d&page=%d",
			gitlabURL, perPage, page)

		log.Printf("Fetching page %d of runners...", page)
		body, err := makeRequest("GET", apiURL, token)
		if err != nil {
			log.Printf("Error fetching runners page %d: %v", page, err)
			return nil, err
		}

		var runners []models.Runner
		if err := json.Unmarshal(body, &runners); err != nil {
			return nil, fmt.Errorf("failed to parse runners JSON: %v", err)
		}

		log.Printf("Fetched %d runners on page %d", len(runners), page)

		// Break if no more runners
		if len(runners) == 0 {
			break
		}

		allRunners = append(allRunners, runners...)

		// If we got fewer runners than perPage, this is the last page
		if len(runners) < perPage {
			break
		}

		page++
	}

	log.Printf("Total runners fetched: %d", len(allRunners))

	// If we've hit the page limit, warn the user
	if page > maxPages {
		log.Printf("WARNING: Hit maximum runner page limit (%d pages). Some runners may not be shown.", maxPages)
	}

	return allRunners, nil
}

// FetchRunner fetches a single runner by its ID
func FetchRunner(gitlabURL, token string, runnerID int) (*models.Runner, error) {
	apiURL := fmt.Sprintf("%s/api/v4/runners/%d", gitlabURL, runnerID)

	log.Printf("Fetching details for runner ID %d", runnerID)
	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		log.Printf("Error fetching runner %d: %v", runnerID, err)
		return nil, err
	}

	var runner models.Runner
	if err := json.Unmarshal(body, &runner); err != nil {
		return nil, fmt.Errorf("failed to parse runner JSON: %v", err)
	}

	return &runner, nil
}

// FetchRunnerProjects now uses the projects embedded in the runner response
// This is more efficient than calling a separate endpoint
func FetchRunnerProjects(gitlabURL, token string, runnerID int) ([]models.Project, error) {
	// Fetch the runner which includes its projects
	runner, err := FetchRunner(gitlabURL, token, runnerID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch runner %d: %v", runnerID, err)
	}

	// Return the projects directly from the runner response
	if runner.Projects != nil && len(runner.Projects) > 0 {
		return runner.Projects, nil
	}

	// If no projects in the response (this is unlikely based on the API),
	// fall back to the separate endpoint
	apiURL := fmt.Sprintf("%s/api/v4/runners/%d/projects?per_page=100",
		gitlabURL, runnerID)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var projects []models.Project
	if err := json.Unmarshal(body, &projects); err != nil {
		return nil, fmt.Errorf("failed to parse runner projects JSON: %v", err)
	}

	return projects, nil
}

// GetProjectsWithRunners fetches all projects that have runners attached
func GetProjectsWithRunners(gitlabURL, token string) ([]models.Project, error) {
	startTime := time.Now()
	log.Printf("Fetching projects with runners...")

	// First get all runners
	runners, err := FetchAllRunners(gitlabURL, token)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch runners: %v", err)
	}

	// Create a map to store unique projects
	projectMap := make(map[int]models.Project)

	// For each runner, get the details including associated projects
	for _, runnerSummary := range runners {
		log.Printf("Fetching details for runner %d (%s)...", runnerSummary.ID, runnerSummary.Description)

		runner, err := FetchRunner(gitlabURL, token, runnerSummary.ID)
		if err != nil {
			log.Printf("Warning: Failed to fetch details for runner %d: %v", runnerSummary.ID, err)
			continue
		}

		// Add projects from the runner response to the map
		for _, project := range runner.Projects {
			projectMap[project.ID] = project
		}
	}

	// Convert map to slice
	projectsWithRunners := make([]models.Project, 0, len(projectMap))
	for _, project := range projectMap {
		projectsWithRunners = append(projectsWithRunners, project)
	}

	log.Printf("Found %d unique projects with runners in %.2f seconds",
		len(projectsWithRunners), time.Since(startTime).Seconds())

	return projectsWithRunners, nil
}
