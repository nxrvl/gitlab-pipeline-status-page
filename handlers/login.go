package handlers

import (
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"gitlab-status/db"
)

// LoginPageHandler handles the login page request
func LoginPageHandler(c echo.Context) error {
	return c.Render(http.StatusOK, "login.html", nil)
}

// LoginSubmitHandler handles the login form submission
func LoginSubmitHandler(c echo.Context, store *sessions.CookieStore) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	// Check if user exists
	user, err := db.GetUserByName(username)
	if err != nil {
		return c.Render(http.StatusUnauthorized, "login.html", map[string]interface{}{
			"Error": "Invalid username or password",
		})
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return c.Render(http.StatusUnauthorized, "login.html", map[string]interface{}{
			"Error": "Invalid username or password",
		})
	}

	// Create session
	session, _ := store.Get(c.Request(), "gitlab-status-session")
	session.Values["logged_in"] = true
	session.Values["username"] = username
	session.Values["user_id"] = user.ID
	if err := session.Save(c.Request(), c.Response()); err != nil {
		return c.Render(http.StatusInternalServerError, "login.html", map[string]interface{}{
			"Error": "Failed to create session",
		})
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
			// Skip authentication for login page
			if c.Path() == "/login" {
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