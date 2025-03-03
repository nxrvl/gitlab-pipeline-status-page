package templates

import (
	"html/template"
	"io"
	"reflect"
	"strings"

	"github.com/labstack/echo/v4"
)

// TemplateRenderer is a custom HTML templating renderer for Echo.
type TemplateRenderer struct {
	templates *template.Template
}

// TemplateFunctions is a map of template functions
var TemplateFunctions = template.FuncMap{
	"lower": strings.ToLower,
	"mul":   func(a, b int) int { return a * b },
	"add":   func(a, b int) int { return a + b },
	"len": func(v interface{}) int {
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice {
			return rv.Len()
		}
		return 0
	},
	"ne": func(a, b interface{}) bool { return a != b },
}

// Render renders a template document.
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

// NewRenderer creates a new template renderer
func NewRenderer() *TemplateRenderer {
	templates := template.New("").Funcs(TemplateFunctions)
	templates = template.Must(templates.ParseGlob("templates/*.html"))

	return &TemplateRenderer{
		templates: templates,
	}
}