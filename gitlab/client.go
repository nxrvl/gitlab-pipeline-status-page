package gitlab

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/uptrace/bun"

	"gitlab-status/models"
)

// Client is a client for GitLab API requests
var Client *http.Client

// Initialize initializes the GitLab API client with the given timeout
func Initialize(timeout time.Duration) {
	log.Printf("Using GitLab API timeout of %v", timeout)
	Client = &http.Client{Timeout: timeout}
}

// makeRequest is a helper function to make GitLab API requests.
func makeRequest(method, url, token string) ([]byte, error) {
	log.Printf("Making GitLab API request: %s %s", method, url)
	startTime := time.Now()

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	// Use the global client with configured timeout
	if Client == nil {
		// Fallback in case the client isn't initialized
		Client = &http.Client{Timeout: 300 * time.Second}
		log.Printf("WARNING: Using fallback HTTP client with 300s timeout")
	}

	resp, err := Client.Do(req)
	if err != nil {
		log.Printf("ERROR: GitLab API request failed after %.2f seconds: %v",
			time.Since(startTime).Seconds(), err)
		return nil, fmt.Errorf("GitLab API request failed: %v (URL: %s)", err, url)
	}
	defer resp.Body.Close()

	log.Printf("GitLab API response received in %.2f seconds with status: %s",
		time.Since(startTime).Seconds(), resp.Status)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		log.Printf("ERROR: GitLab API non-OK response: %s - Body: %s", resp.Status, string(bodyBytes))
		return nil, fmt.Errorf("GitLab API request failed with status %s (URL: %s)", resp.Status, url)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Error reading GitLab API response: %v", err)
	}

	log.Printf("GitLab API request completed in %.2f seconds, response size: %d bytes",
		time.Since(startTime).Seconds(), len(body))

	return body, nil
}

// FetchGroups gets all GitLab groups accessible with the token
func FetchGroups(gitlabURL, token string) ([]models.Group, error) {
	page := 1
	perPage := 100 // Maximum allowed by GitLab API
	maxPages := 5  // Limit number of pages to fetch to avoid extremely long requests
	allGroups := []models.Group{}

	log.Printf("Fetching GitLab groups from %s", gitlabURL)

	for page <= maxPages {
		// Get all top-level groups
		apiURL := fmt.Sprintf("%s/api/v4/groups?per_page=%d&page=%d&order_by=name&sort=asc&all_available=true",
			gitlabURL, perPage, page)

		log.Printf("Fetching page %d of groups...", page)
		body, err := makeRequest("GET", apiURL, token)
		if err != nil {
			log.Printf("Error fetching groups page %d: %v", page, err)
			return nil, err
		}

		var groups []models.Group
		if err := json.Unmarshal(body, &groups); err != nil {
			return nil, fmt.Errorf("failed to parse groups JSON: %v", err)
		}

		log.Printf("Fetched %d groups on page %d", len(groups), page)

		// Break if no more groups
		if len(groups) == 0 {
			break
		}

		allGroups = append(allGroups, groups...)

		// If we got fewer groups than perPage, this is the last page
		if len(groups) < perPage {
			break
		}

		page++
	}

	log.Printf("Total groups fetched: %d", len(allGroups))

	// If we've hit the page limit, warn the user
	if page > maxPages {
		log.Printf("WARNING: Hit maximum group page limit (%d pages). Some groups may not be shown.", maxPages)
	}

	return allGroups, nil
}

// FetchSubgroups gets all subgroups for a specific group
func FetchSubgroups(gitlabURL, token string, groupID int) ([]models.Group, error) {
	apiURL := fmt.Sprintf("%s/api/v4/groups/%d/subgroups?per_page=100&order_by=name&sort=asc&all_available=true",
		gitlabURL, groupID)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var subgroups []models.Group
	if err := json.Unmarshal(body, &subgroups); err != nil {
		return nil, fmt.Errorf("failed to parse subgroups JSON: %v", err)
	}

	return subgroups, nil
}

// FetchGroupProjects gets all projects for a specific group
func FetchGroupProjects(gitlabURL, token string, groupID int) ([]models.Project, error) {
	apiURL := fmt.Sprintf("%s/api/v4/groups/%d/projects?per_page=100&order_by=name&sort=asc&include_subgroups=false",
		gitlabURL, groupID)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var projects []models.Project
	if err := json.Unmarshal(body, &projects); err != nil {
		return nil, fmt.Errorf("failed to parse group projects JSON: %v", err)
	}

	return projects, nil
}

// BuildGroupTree recursively builds a hierarchical tree of groups with their projects
func BuildGroupTree(gitlabURL, token string, groups []models.Group, parentID int, level int) ([]models.Group, error) {
	var result []models.Group

	if level == 0 {
		log.Printf("Building group tree with %d top-level groups", len(groups))
	}

	for i, group := range groups {
		if group.ParentID == parentID {
			// Log progress for top-level groups only to avoid log spam
			if level == 0 && len(groups) > 5 {
				if i == 0 || i == len(groups)-1 || i%(len(groups)/5) == 0 {
					log.Printf("Processing group %d/%d (%d%%): %s", i+1, len(groups),
						(i+1)*100/len(groups), group.Name)
				}
			}

			// Set the indentation level for UI
			group.Level = level

			// Get subgroups
			subgroups, err := FetchSubgroups(gitlabURL, token, group.ID)
			if err != nil {
				log.Printf("Warning: Failed to fetch subgroups for group %s: %v", group.Name, err)
			} else if len(subgroups) > 0 {
				// Log subgroups if there are many
				if len(subgroups) > 10 {
					log.Printf("Found %d subgroups for %s, processing...", len(subgroups), group.Name)
				}

				// Recursively build tree for subgroups
				group.Subgroups, err = BuildGroupTree(gitlabURL, token, subgroups, 0, level+1)
				if err != nil {
					log.Printf("Warning: Failed to build subgroup tree for group %s: %v", group.Name, err)
				}
			}

			// Get projects for this group
			projectStartTime := time.Now()
			projects, err := FetchGroupProjects(gitlabURL, token, group.ID)
			if err != nil {
				log.Printf("Warning: Failed to fetch projects for group %s: %v", group.Name, err)
			} else {
				// Log projects if there are many
				if len(projects) > 20 {
					log.Printf("Found %d projects for group %s in %.2f seconds",
						len(projects), group.Name, time.Since(projectStartTime).Seconds())
				}

				// Set level for each project for UI indentation
				for i := range projects {
					projects[i].Level = level + 1
				}
				group.Projects = projects
			}

			// Mark if this group has children (subgroups or projects)
			group.HasChildren = len(group.Subgroups) > 0 || len(group.Projects) > 0

			// Set expanded state
			group.Expanded = level == 0 // Top-level groups are expanded by default

			result = append(result, group)
		}
	}

	return result, nil
}

// FetchProjects gets the list of all GitLab projects accessible with the token.
func FetchProjects(gitlabURL, token string) ([]models.Project, error) {
	// Get projects with pagination to ensure we get all projects
	page := 1
	perPage := 100 // Maximum allowed by GitLab API
	maxPages := 10 // Limit number of pages to fetch to avoid extremely long requests
	allProjects := []models.Project{}

	log.Printf("Fetching GitLab projects from %s", gitlabURL)

	for page <= maxPages {
		apiURL := fmt.Sprintf("%s/api/v4/projects?per_page=%d&page=%d&order_by=name&sort=asc&membership=true",
			gitlabURL, perPage, page)

		log.Printf("Fetching page %d of projects...", page)
		body, err := makeRequest("GET", apiURL, token)
		if err != nil {
			log.Printf("Error fetching projects page %d: %v", page, err)
			return nil, err
		}

		var projects []models.Project
		if err := json.Unmarshal(body, &projects); err != nil {
			return nil, fmt.Errorf("failed to parse projects JSON: %v", err)
		}

		log.Printf("Fetched %d projects on page %d", len(projects), page)

		// Break if no more projects
		if len(projects) == 0 {
			break
		}

		allProjects = append(allProjects, projects...)

		// If we got fewer projects than perPage, this is the last page
		if len(projects) < perPage {
			break
		}

		page++
	}

	log.Printf("Total projects fetched: %d", len(allProjects))

	// If we've hit the page limit, warn the user
	if page > maxPages {
		log.Printf("WARNING: Hit maximum page limit (%d pages). Some projects may not be shown.", maxPages)
	}

	return allProjects, nil
}

// FetchLatestPipeline calls the GitLab API to get the latest pipeline for a project.
func FetchLatestPipeline(gitlabURL, projectID, token string) (*models.Pipeline, error) {
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/pipelines?per_page=1", gitlabURL, projectID)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var pipelines []models.Pipeline
	if err := json.Unmarshal(body, &pipelines); err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return nil, fmt.Errorf("no pipelines found for project %s", projectID)
	}
	return &pipelines[0], nil
}

// FetchPipelines gets multiple pipelines for a project.
func FetchPipelines(gitlabURL, projectID, token string, count int) ([]models.Pipeline, error) {
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/pipelines?per_page=%d", gitlabURL, projectID, count)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var pipelines []models.Pipeline
	if err := json.Unmarshal(body, &pipelines); err != nil {
		return nil, err
	}

	return pipelines, nil
}

// FetchLastSuccessPipeline gets the last successful pipeline for a project.
func FetchLastSuccessPipeline(gitlabURL, projectID, token string) (*models.Pipeline, error) {
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/pipelines?per_page=20&status=success", gitlabURL, projectID)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var pipelines []models.Pipeline
	if err := json.Unmarshal(body, &pipelines); err != nil {
		return nil, err
	}

	if len(pipelines) == 0 {
		return nil, nil // No successful pipelines found
	}

	return &pipelines[0], nil
}

// GetProject fetches a single project by ID or path.
func GetProject(gitlabURL, projectPath, token string) (*models.Project, error) {
	encodedProjectPath := url.PathEscape(projectPath)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", gitlabURL, encodedProjectPath)

	body, err := makeRequest("GET", apiURL, token)
	if err != nil {
		return nil, err
	}

	var project models.Project
	if err := json.Unmarshal(body, &project); err != nil {
		return nil, err
	}

	return &project, nil
}

// CacheGitLabStructure fetches all groups and projects from GitLab and stores them in the database
func CacheGitLabStructure(db *bun.DB, userID int64, gitlabURL, token string) error {
	log.Printf("Starting to cache GitLab structure for user ID %d from %s", userID, gitlabURL)
	startTime := time.Now()

	// Get the groups
	groups, err := FetchGroups(gitlabURL, token)
	if err != nil {
		log.Printf("Error fetching groups: %v", err)
		return err
	}
	log.Printf("Successfully fetched %d groups", len(groups))

	// Get the projects
	projects, err := FetchProjects(gitlabURL, token)
	if err != nil {
		log.Printf("Error fetching projects: %v", err)
		return err
	}
	log.Printf("Successfully fetched %d projects", len(projects))

	// Store the data in the database (this would be handled by db package)
	err = storeInDatabase(db, userID, groups, projects)
	if err != nil {
		return err
	}

	log.Printf("Successfully cached GitLab structure for user %d: %d groups, %d projects in %.2f seconds",
		userID, len(groups), len(projects), time.Since(startTime).Seconds())

	return nil
}

// storeInDatabase stores the fetched GitLab data in the database
func storeInDatabase(db *bun.DB, userID int64, groups []models.Group, projects []models.Project) error {
	// Implementation to store data in database
	// This should be moved to the db package in a proper implementation
	return nil
}
