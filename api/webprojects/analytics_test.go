package webprojects

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnalyticsDocumentClassification(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, accept, dest, purpose string
		status                                    int
		want                                      bool
	}{
		{"document", "GET", "index.html", "text/html", "document", "", 200, true},
		{"frame", "GET", "frame.htm", "text/html", "iframe", "", 200, true},
		{"legacy", "GET", "index.html", "text/html", "", "", 200, true},
		{"conditional", "GET", "index.html", "text/html", "document", "", 304, true},
		{"head", "HEAD", "index.html", "text/html", "document", "", 200, false},
		{"asset", "GET", "main.js", "*/*", "script", "", 200, false},
		{"html fetch", "GET", "index.html", "text/html", "empty", "", 200, false},
		{"prefetch", "GET", "index.html", "text/html", "document", "prefetch", 200, false},
		{"partial", "GET", "index.html", "text/html", "document", "", 206, false},
		{"failed", "GET", "index.html", "text/html", "document", "", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "https://home.example.com/p/page/"+tc.path, nil)
			request.Header.Set("Accept", tc.accept)
			request.Header.Set("Sec-Fetch-Dest", tc.dest)
			request.Header.Set("Purpose", tc.purpose)
			if got := isAnalyticsDocument(request, tc.path, tc.status); got != tc.want {
				t.Fatalf("record=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestAnalyticsCookieIsIndependentAndHostOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "https://home.example.com/p/page/", nil)
	first, err := analyticsVisitor(c)
	if err != nil || len(first) != 32 {
		t.Fatal("random visitor ID was not issued")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("one independent cookie expected")
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-cq_visit" || !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe visitor cookie: %+v", cookie)
	}
	c.Request.AddCookie(cookie)
	second, err := analyticsVisitor(c)
	if err != nil || first != second {
		t.Fatal("existing visitor should keep identity")
	}
}
