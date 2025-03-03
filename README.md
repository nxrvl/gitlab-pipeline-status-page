# GitLab Pipeline Status Viewer

A web application for monitoring the status of multiple GitLab pipelines in one place.

## Features

- **User Authentication**: Secure login system with password encryption
- **Project Selection**: Choose which GitLab projects to monitor
- **Status Dashboard**: View pipeline status with auto-refresh
- **Pipeline History**: Hover to see recent pipeline history (last 10 pipelines)
- **Interactive Links**: Click to view project or pipeline details in GitLab
- **Persistent Storage**: SQLite database with Bun ORM for storing user preferences

## Project Structure

The project has been organized into the following modules for better maintainability:

- `models/` - Data structures and database models
- `handlers/` - HTTP route handlers for each page
- `gitlab/` - GitLab API client and related functions
- `templates/` - HTML templates and template renderer
- `db/` - Database setup and operations
- `main.go` - Application setup and entry point

## Requirements

- Go 1.16+
- GitLab API access token

## Installation

### Standard Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd gitlab-status
```

2. Install dependencies:
```bash
go mod tidy
```

3. Set up environment variables in a `.env` file:
```
GITLAB_URL=https://your-gitlab-instance.com
GITLAB_TOKEN=your_personal_access_token
DEFAULT_USERNAME=admin
DEFAULT_PASSWORD=your_secure_password
SESSION_SECRET=your_session_secret
```

4. Build and run the application:
```bash
go build -o gitlab-status
./gitlab-status
```

### Docker Installation

1. Build the Docker image:
```bash
docker build -t gitlab-status .
```

2. Run the container with a persistent volume for the database:
```bash
docker run -d \
  -p 8080:8080 \
  -v gitlab-status-data:/data \
  -e GITLAB_URL=https://your-gitlab-instance.com \
  -e GITLAB_TOKEN=your_personal_access_token \
  -e DEFAULT_USERNAME=admin \
  -e DEFAULT_PASSWORD=your_secure_password \
  -e SESSION_SECRET=your_session_secret \
  --name gitlab-status \
  gitlab-status
```

## Usage

1. Access the application at http://localhost:8080

2. Log in with the default credentials:
   - Username: admin (or your configured DEFAULT_USERNAME)
   - Password: password (or your configured DEFAULT_PASSWORD)

3. Go to Settings to select which GitLab projects to monitor

4. View the status dashboard to see pipeline status for all selected projects

## Environment Variables

- `GITLAB_URL`: URL of your GitLab instance (default: https://gitlab.example.com)
- `GITLAB_TOKEN`: GitLab personal access token (required)
- `GITLAB_API_TIMEOUT`: Timeout in seconds for GitLab API requests (default: 300)
- `DEFAULT_USERNAME`: Default admin username (default: admin)
- `DEFAULT_PASSWORD`: Default admin password (default: password)
- `SESSION_SECRET`: Secret for session cookies (default: mysessionsecret)
- `DB_PATH`: Path to SQLite database file (default: gitlab-status.db, in Docker: /data/gitlab-status.db)
- `PORT`: Port to run the application on (default: 8080)

## Tech Stack

- Go
  - Echo web framework
  - BUN SQLite ORM
  - HTMX for dynamic content
- Frontend
  - Bootstrap 5 for styling
  - HTMX for dynamic updates

## Development

- Run the application in development mode:
  ```
  go run main.go
  ```

- Format code:
  ```
  go fmt ./...
  ```

- Update dependencies:
  ```
  go mod tidy
  ```

## Security Notes

- Change the default password after first login
- Use a strong SESSION_SECRET in production
- HTTPS is recommended for production use

## License

[MIT](LICENSE)