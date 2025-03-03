package handlers

import (
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"gitlab-status/db"
	"gitlab-status/templates"
)

// LoginPageHandler handles the login page request
func LoginPageHandler(c echo.Context) error {
	return templates.Login("").Render(c.Request().Context(), c.Response().Writer)
}

// LoginSubmitHandler handles the login form submission
func LoginSubmitHandler(c echo.Context, store *sessions.CookieStore) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	// Check if user exists
	user, err := db.GetUserByName(username)
	if err != nil {
		return templates.Login("Invalid username or password").Render(c.Request().Context(), c.Response().Writer)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return templates.Login("Invalid username or password").Render(c.Request().Context(), c.Response().Writer)
	}

	// Create session
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	session.Values["logged_in"] = true
	session.Values["username"] = username
	session.Values["user_id"] = user.ID
	if err := session.Save(c.Request(), c.Response()); err != nil {
		return templates.Login("Failed to create session").Render(c.Request().Context(), c.Response().Writer)
	}

	// Redirect to status page
	return c.Redirect(http.StatusSeeOther, "/")
}

// LogoutHandler handles the logout request
func LogoutHandler(c echo.Context, store *sessions.CookieStore) error {
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	session.Values["logged_in"] = false
	session.Values["username"] = ""
	session.Save(c.Request(), c.Response())
	return c.Redirect(http.StatusSeeOther, "/login")
}

// AuthMiddleware checks if a user is authenticated
func AuthMiddleware(store *sessions.CookieStore) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip authentication for login page and static assets
			if c.Path() == "/login" || c.Path() == "/favicon.ico" {
				return next(c)
			}

			session, err := store.Get(c.Request(), "gitlab-status-session")
			if err != nil {
				// Session error, redirect to login
				return c.Redirect(http.StatusSeeOther, "/login")
			}

			// Check if user is logged in
			isLoggedIn, ok := session.Values["logged_in"].(bool)
			if !ok || !isLoggedIn {
				// Not logged in, redirect to login
				return c.Redirect(http.StatusSeeOther, "/login")
			}

			// Continue with the request
			return next(c)
		}
	}
}
