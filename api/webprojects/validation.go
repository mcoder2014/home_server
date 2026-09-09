package webprojects

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

func parseIfMatch(value string) (int64, error) {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return 0, fmt.Errorf("valid If-Match revision is required")
	}
	return revision, nil
}

func validateProjectTarget(target string) error {
	if target == "" || strings.ContainsAny(target, "\\\r\n\x00") || strings.HasPrefix(target, "//") {
		return fmt.Errorf("invalid project target")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.RawPath != "" || !strings.HasPrefix(parsed.Path, "/p/") || strings.ContainsAny(parsed.Path, "\\\x00") {
		return fmt.Errorf("invalid project target")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid project target")
		}
	}
	if path.Clean(parsed.Path) != strings.TrimSuffix(parsed.Path, "/") {
		return fmt.Errorf("invalid project target")
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/p/"), "/")
	if len(parts) == 0 || !validSlug(parts[0]) || path.Clean(parsed.Path) == "/p" {
		return fmt.Errorf("invalid project target")
	}
	return nil
}

func isDocumentNavigation(method, requestPath, accept string) bool {
	if method != http.MethodGet || !strings.Contains(strings.ToLower(accept), "text/html") {
		return false
	}
	ext := strings.ToLower(path.Ext(requestPath))
	return ext == "" || ext == ".html" || ext == ".htm"
}

func validSlug(slug string) bool {
	if len(slug) < 3 || len(slug) > 48 || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	for _, char := range slug {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
