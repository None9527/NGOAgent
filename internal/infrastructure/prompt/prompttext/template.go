package prompttext

import (
	"strings"
	"text/template"
)

// Render applies template variables to a prompt text constant.
// Used for ephemeral messages that contain {{.Var}} placeholders.
func Render(tmplText string, data any) string {
	tmpl, err := template.New("").Parse(tmplText)
	if err != nil {
		return tmplText // Return raw text if template is invalid
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return tmplText
	}
	return buf.String()
}

// Wrap wraps content in XML-style tags. Returns empty string if content is empty.
func Wrap(tag, content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	return "<" + tag + ">\n" + strings.TrimSpace(content) + "\n</" + tag + ">"
}
