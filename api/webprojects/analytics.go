package webprojects

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcoder2014/home_server/app/acceleration"
	application "github.com/mcoder2014/home_server/app/webprojects"
	"github.com/mcoder2014/home_server/domain/service/webanalytics"
	service "github.com/mcoder2014/home_server/domain/service/webprojects"
	"github.com/mcoder2014/home_server/utils/ginfmt"
)

// projectStatistics keeps statistics private even for a public hosted page.
// Ownership is checked before disabled/not-started responses to avoid leaking
// project existence. Only committed SQL snapshots are returned to the browser.
func projectStatistics(c *gin.Context) {
	id, err := service.ParsePositiveID(c.Param("id"))
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	days := 30
	if text := c.Query("days"); text != "" {
		days, err = strconv.Atoi(text)
		if err != nil {
			ginfmt.Fail(c, service.ErrInvalid)
			return
		}
	}
	if days != 7 && days != 30 && days != 90 {
		ginfmt.Fail(c, service.ErrInvalid)
		return
	}
	project, err := application.Default.GetOwnedProject(currentUserID(c), id)
	if err != nil {
		ginfmt.Fail(c, err)
		return
	}
	if project.Status == "deleted" {
		ginfmt.Fail(c, service.ErrNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	runtime := acceleration.Current.Load()
	if runtime == nil || runtime.Analytics == nil {
		ginfmt.Success(c, http.StatusOK, &webanalytics.Stats{ProjectID: id, Timezone: "Asia/Shanghai", UVMethod: "browser_hll", Daily: []webanalytics.DailyStats{}, Quality: "disabled", QualityReason: "analytics_disabled"})
		return
	}
	result, err := runtime.Analytics.Stats(ginfmt.RPCContext(c), id, days)
	if err != nil {
		ginfmt.Fail(c, service.ErrDependency)
		return
	}
	ginfmt.Success(c, http.StatusOK, result)
}

// isAnalyticsDocument defines a server-observable page view, not a unique human
// or completed reading. HTML fetches, partial bodies and static assets do not
// count; a valid conditional document response does count as another view.
func isAnalyticsDocument(request *http.Request, file string, status int) bool {
	if request.Method != http.MethodGet || (status != http.StatusOK && status != http.StatusNotModified) {
		return false
	}
	extension := strings.ToLower(filepath.Ext(file))
	if extension != ".html" && extension != ".htm" {
		return false
	}
	purpose := strings.ToLower(request.Header.Get("Purpose") + " " + request.Header.Get("Sec-Purpose"))
	if strings.Contains(purpose, "prefetch") || strings.Contains(purpose, "prerender") {
		return false
	}
	agent := strings.ToLower(request.UserAgent())
	for _, bot := range []string{"googlebot", "bingbot", "baiduspider", "yandexbot", "duckduckbot"} {
		if strings.Contains(agent, bot) {
			return false
		}
	}
	destination := strings.ToLower(request.Header.Get("Sec-Fetch-Dest"))
	if destination != "" {
		return destination == "document" || destination == "iframe"
	}
	return strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/html")
}

// analyticsVisitor uses a separate host-only cookie; no account, IP, login
// cookie or application credential participates in browser UV identity.
func analyticsVisitor(c *gin.Context) (string, error) {
	found := ""
	count := 0
	for _, cookie := range c.Request.Cookies() {
		if cookie.Name == "__Host-cq_visit" {
			count++
			found = cookie.Value
		}
	}
	decoded, err := hex.DecodeString(found)
	if count == 1 && len(found) == 32 && err == nil && len(decoded) == 16 {
		return strings.ToLower(found), nil
	}
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	found = hex.EncodeToString(value)
	http.SetCookie(c.Writer, &http.Cookie{Name: "__Host-cq_visit", Value: found, Path: "/", MaxAge: 365 * 24 * 60 * 60, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return found, nil
}
