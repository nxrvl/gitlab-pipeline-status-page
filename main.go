package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Pipeline represents a simplified GitLab pipeline.
type Pipeline struct {
	ID        int       `json:"id"`
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// RepositoryStatus holds the data to be displayed for each repository.
type RepositoryStatus struct {
	RepositoryName string
	Version        string
	PipelineID     int
	Status         string
	Date           time.Time
}

// TemplateRenderer is a custom HTML templating renderer for Echo.
type TemplateRenderer struct {
	templates *template.Template
}

// Render renders a template document.
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

// fetchLatestPipeline calls the GitLab API to get the latest pipeline for a project.
func fetchLatestPipeline(gitlabURL, project, token string) (*Pipeline, error) {
	encodedProject := url.PathEscape(project)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/pipelines?per_page=1", gitlabURL, encodedProject)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch pipeline for project %s: %s", project, resp.Status)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var pipelines []Pipeline
	if err := json.Unmarshal(body, &pipelines); err != nil {
		return nil, err
	}
	if len(pipelines) == 0 {
		return nil, fmt.Errorf("no pipelines found for project %s", project)
	}
	return &pipelines[0], nil
}

func main() {
	// Load environment variables from .env file.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, proceeding with system environment variables")
	}

	// Get configuration from environment variables.
	gitlabURL := os.Getenv("GITLAB_URL")
	if gitlabURL == "" {
		gitlabURL = "https://gitlab.example.com" // update with your GitLab instance URL
	}
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		log.Fatal("GITLAB_TOKEN not set")
	}

	// Get basic auth credentials.
	authUser := os.Getenv("BASIC_AUTH_USER")
	if authUser == "" {
		authUser = "admin"
	}
	authPass := os.Getenv("BASIC_AUTH_PASS")
	if authPass == "" {
		authPass = "password"
	}

	// Get repositories list from environment (comma separated).
	var repositories []string
	if reposStr := os.Getenv("REPOSITORIES"); reposStr != "" {
		repositories = strings.Split(reposStr, ",")
	} else {
		repositories = []string{
			"group1/project1",
			"group2/project2",
		}
	}

	// Initialize Echo.
	e := echo.New()

	// Setup basic auth middleware.
	e.Use(middleware.BasicAuth(func(user, pass string, c echo.Context) (bool, error) {
		if user == authUser && pass == authPass {
			return true, nil
		}
		return false, nil
	}))

	// Initialize the templating renderer using the "templ" library (Go's html/template).
	renderer := &TemplateRenderer{
		templates: template.Must(template.ParseGlob("templates/*.html")),
	}
	e.Renderer = renderer

	// Handler for the status page.
	e.GET("/", func(c echo.Context) error {
		var statuses []RepositoryStatus
		for _, repo := range repositories {
			pipeline, err := fetchLatestPipeline(gitlabURL, strings.TrimSpace(repo), token)
			if err != nil {
				log.Printf("Error fetching pipeline for %s: %v", repo, err)
				// Display a row with error info if pipeline fetch fails.
				statuses = append(statuses, RepositoryStatus{
					RepositoryName: repo,
					Version:        "N/A",
					PipelineID:     0,
					Status:         "Error",
					Date:           time.Time{},
				})
				continue
			}
			statuses = append(statuses, RepositoryStatus{
				RepositoryName: repo,
				Version:        pipeline.Ref,
				PipelineID:     pipeline.ID,
				Status:         pipeline.Status,
				Date:           pipeline.CreatedAt,
			})
		}

		// If the request is an HTMX request, render the partial.
		if c.Request().Header.Get("HX-Request") != "" {
			return c.Render(http.StatusOK, "status_partial.html", statuses)
		}
		return c.Render(http.StatusOK, "status.html", statuses)
	})

	// Determine the port.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	e.Logger.Fatal(e.Start(":" + port))
}
