package manuals

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManualCategoriesUseBinaryCursorPagination(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	requireManualSuccess(t, fixture.request(http.MethodPost, "/api/manuals", fixture.owner, map[string]interface{}{
		"name": "分类分页", "categories": []string{"厨房", "a", "A"}, "access_mode": "owner", "client_request_id": "category-page-manual",
	}, nil))

	first := requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/categories?mine=true&limit=2", fixture.owner, nil, nil))
	require.Equal(t, []interface{}{"A", "a"}, first["items"])
	require.Equal(t, true, first["has_more"])
	require.Equal(t, "a", first["next_cursor"])

	second := requireManualSuccess(t, fixture.request(http.MethodGet, "/api/manuals/categories?mine=true&limit=2&cursor="+url.QueryEscape(first["next_cursor"].(string)), fixture.owner, nil, nil))
	require.Equal(t, []interface{}{"厨房"}, second["items"])
	require.Equal(t, false, second["has_more"])
	require.Equal(t, "", second["next_cursor"])

	requireManualDenied(t, fixture.request(http.MethodGet, "/api/manuals/categories?mine=true&limit=1001", fixture.owner, nil, nil))
}
