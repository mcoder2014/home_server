package log

import "net/http"

// RedactedHeaders keeps diagnostic request headers while excluding credentials.
// Unknown/custom headers default to redacted so future authentication mechanisms
// cannot silently leak through the WebDAV error logger.
func RedactedHeaders(headers http.Header) http.Header {
	result := make(http.Header, len(headers))
	for name, values := range headers {
		switch http.CanonicalHeaderKey(name) {
		case "Content-Type", "Content-Length", "Depth", "Overwrite", "User-Agent":
			result[name] = append([]string(nil), values...)
		default:
			result[name] = []string{"[redacted]"}
		}
	}
	return result
}
