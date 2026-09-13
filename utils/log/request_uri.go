package log

import (
	"net/url"
	"regexp"
	"strings"
)

var credentialValue = regexp.MustCompile(`(?:ak|sk|at)_cq_[A-Za-z0-9_-]+`)

// RedactedURI protects even rejected requests: credentials in a query are never
// a supported auth mechanism, but the access logger still sees their original URI.
func RedactedURI(value string) string {
	value = credentialValue.ReplaceAllString(value, "[redacted]")
	parts := strings.SplitN(value, "?", 2)
	if len(parts) != 2 {
		return value
	}
	if parts[0] == "/api/auth/token" {
		return parts[0] + "?[redacted]"
	}
	query, err := url.ParseQuery(parts[1])
	if err != nil {
		return parts[0] + "?[redacted]"
	}
	changed := false
	for name := range query {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "passwd") || lower == "passport" || lower == "authorization" || lower == "signature" || lower == "client_id" || lower == "api_key" || lower == "access_key" || lower == "ak" || lower == "sk" {
			query.Set(name, "[redacted]")
			changed = true
		}
		for _, value := range query[name] {
			if credentialValue.MatchString(value) {
				query.Set(name, "[redacted]")
				changed = true
				break
			}
		}
	}
	if !changed {
		return value
	}
	return parts[0] + "?" + query.Encode()
}
