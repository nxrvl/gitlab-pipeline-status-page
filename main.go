package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"gitlab-status/db"
	"gitlab-status/gitlab"
	"gitlab-status/handlers"
	"gitlab-status/templates"
)

func main() {
	// Load environment variables from .env file.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, proceeding with system environment variables")
	}

	// Get configuration from environment variables.
	gitlabURL := os.Getenv("GITLAB_URL")
	if gitlabURL == "" {
		gitlabURL = "https://gitlab.example.com" // update with your GitLab instance URL
		log.Printf("GITLAB_URL not set, using default: %s", gitlabURL)
	}
	log.Printf("Using GitLab URL: %s", gitlabURL)
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		log.Fatal("GITLAB_TOKEN not set")
	}

	// Get API timeout from environment
	timeoutStr := os.Getenv("GITLAB_API_TIMEOUT")
	timeout := 300 * time.Second // Default timeout: 300 seconds
	if timeoutStr != "" {
		if timeoutSec, err := strconv.Atoi(timeoutStr); err == nil && timeoutSec > 0 {
			timeout = time.Duration(timeoutSec) * time.Second
			log.Printf("Setting GitLab API timeout to %d seconds", timeoutSec)
		}
	}

	// Initialize GitLab client
	gitlab.Initialize(timeout)

	// Set up SQLite database
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "gitlab-status.db" // Default SQLite database file
	}

	// Initialize database
	if err := db.Initialize(dbPath); err != nil {
		log.Fatal("Failed to initialize database: ", err)
	}

	// Set up initial user
	defaultUser := os.Getenv("DEFAULT_USERNAME")
	if defaultUser == "" {
		defaultUser = "admin"
	}
	defaultPass := os.Getenv("DEFAULT_PASSWORD")
	if defaultPass == "" {
		defaultPass = "password"
	}

	if err := db.CreateDefaultUser(defaultUser, defaultPass); err != nil {
		log.Fatal("Failed to create default user: ", err)
	}

	// Start background job to update cache every 30 minutes
	startBackgroundCacheJob(gitlabURL, token)

	// Get session secret
	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "mysessionsecret" // Should be changed in production
	}

	// Initialize the session store
	store := sessions.NewCookieStore([]byte(sessionSecret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
	}

	// Initialize Echo.
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Set up template renderer
	e.Renderer = templates.NewRenderer()

	// Set up middleware
	e.Use(handlers.AuthMiddleware(store))

	// Set up routes
	// Authentication routes
	e.GET("/login", func(c echo.Context) error {
		return handlers.LoginPageHandler(c)
	})
	e.POST("/login", func(c echo.Context) error {
		return handlers.LoginSubmitHandler(c, store)
	})
	e.GET("/logout", func(c echo.Context) error {
		return handlers.LogoutHandler(c, store)
	})

	// Status page route
	e.GET("/", func(c echo.Context) error {
		return handlers.StatusPageHandler(c, store, gitlabURL, token)
	})

	// Settings routes
	e.GET("/settings", func(c echo.Context) error {
		return handlers.SettingsPageHandler(c, store)
	})
	e.GET("/settings/projects", func(c echo.Context) error {
		return handlers.ProjectsPageHandler(c, store)
	})
	e.POST("/settings/cache", func(c echo.Context) error {
		return handlers.StartCacheHandler(c, store, gitlabURL, token)
	})
	e.GET("/settings/cache/status", func(c echo.Context) error {
		return handlers.CacheStatusHandler(c)
	})
	e.POST("/settings", func(c echo.Context) error {
		return handlers.SaveSettingsHandler(c, store)
	})

	// Determine the port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start the server
	e.Logger.Fatal(e.Start(":" + port))
}

// startBackgroundCacheJob starts a goroutine that updates the project structure every 30 minutes
func startBackgroundCacheJob(gitlabURL, token string) {
	log.Println("Starting background job to update project structure every 30 minutes")

	ticker := time.NewTicker(30 * time.Minute)

	// Run the first update immediately
	go func() {
		log.Printf("Running initial GitLab structure cache")
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

	// Run updates on schedule
	go func() {
		for range ticker.C {
			log.Printf("Running scheduled GitLab structure cache update")
			// Fetch groups and projects
			groups, err := gitlab.FetchGroups(gitlabURL, token)
			if err != nil {
				log.Printf("Error fetching groups: %v", err)
				continue
			}

			projects, err := gitlab.FetchProjects(gitlabURL, token)
			if err != nil {
				log.Printf("Error fetching projects: %v", err)
				continue
			}

			// Store in database
			err = db.CacheGitLabStructure(groups, projects)
			if err != nil {
				log.Printf("Error caching GitLab structure: %v", err)
			}
		}
	}()
}
