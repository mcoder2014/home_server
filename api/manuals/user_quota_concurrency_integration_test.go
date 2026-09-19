package manuals

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	manualapplication "github.com/mcoder2014/home_server/app/manuals"
	"github.com/mcoder2014/home_server/config"
	manualservice "github.com/mcoder2014/home_server/domain/service/manuals"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/mcoder2014/home_server/utils"
	"github.com/stretchr/testify/require"
)

func TestManualConcurrentUserByteQuotaAcrossManuals(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	manualIDs := []string{
		createManualForWriteGuard(t, fixture, "user-byte-manual-1"),
		createManualForWriteGuard(t, fixture, "user-byte-manual-2"),
	}
	conf := config.Global()
	conf.Manuals.MaxUserBytes = 5
	config.SetGlobalConfig(conf)

	bodies := make([]*manualBlockedBody, 2)
	done := make([]<-chan manualHTTPResponse, 2)
	for index, manualID := range manualIDs {
		payload, err := json.Marshal(map[string]interface{}{
			"kind": "text", "title": "用户配额竞争", "text": "12345", "client_request_id": "user-byte-item-" + strconv.Itoa(index+1),
		})
		require.NoError(t, err)
		bodies[index], done[index] = beginBlockedManualRequest(t, fixture, http.MethodPost, "/api/manuals/"+manualID+"/items", fixture.owner, payload, nil)
	}
	for _, body := range bodies {
		body.unblock()
	}
	responses := []manualHTTPResponse{
		finishBlockedManualRequest(t, bodies[0], done[0]),
		finishBlockedManualRequest(t, bodies[1], done[1]),
	}

	successes, limited := 0, 0
	for _, response := range responses {
		switch response.status {
		case http.StatusCreated:
			successes++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected user-quota response: status=%d code=%d body=%s", response.status, response.code, response.body)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, limited)
	require.Equal(t, int64(1), countManualRows(t, fixture, "SELECT COUNT(*) FROM manual_items WHERE manual_id IN (?, ?)", manualIDs[0], manualIDs[1]))
	var totalBytes, revisionSum int64
	require.NoError(t, fixture.database.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM manual_items WHERE manual_id IN (?, ?)", manualIDs[0], manualIDs[1]).Scan(&totalBytes))
	require.NoError(t, fixture.database.QueryRow("SELECT SUM(revision) FROM manuals WHERE id IN (?, ?)", manualIDs[0], manualIDs[1]).Scan(&revisionSum))
	require.Equal(t, int64(5), totalBytes)
	require.Equal(t, int64(3), revisionSum, "one successful append must increment exactly one manual revision")
}

func TestManualDatabaseUserByteQuotaAcrossSessionsAndApplicationInstances(t *testing.T) {
	fixture := newManualHTTPFixture(t)
	manualIDs := make([]int64, 2)
	for index, requestID := range []string{"database-user-byte-manual-1", "database-user-byte-manual-2"} {
		value, err := strconv.ParseInt(createManualForWriteGuard(t, fixture, requestID), 10, 64)
		require.NoError(t, err)
		manualIDs[index] = value
	}
	conf := config.Global()
	conf.Manuals.MaxUserBytes = 5
	config.SetGlobalConfig(conf)

	secondToken := "second-owner-session-token"
	digest := sha256.Sum256([]byte(secondToken))
	now := time.Now().UTC()
	_, err := fixture.database.Exec("INSERT INTO login_token (id, user_id, token_digest, auth_version, purpose, is_expired, authenticated_at, login_ip, user_agent, client_name, os_name, device_type, login_source, expire_time, create_time, update_time) VALUES (?, ?, ?, 1, 'user', 0, ?, '127.0.0.1', 'manual-test', 'test', 'test', 'test', 'test', ?, ?, ?)", fixture.ownerID+10_000, fixture.ownerID, digest[:], now, now.Add(time.Hour), now, now)
	require.NoError(t, err)

	tokens := []string{fixture.owner.cookie.Value, secondToken}
	applications := []*manualapplication.Application{manualapplication.New(), manualapplication.New()}
	start := make(chan struct{})
	ready := make(chan struct{}, len(applications))
	results := make(chan error, len(applications))
	for index := range applications {
		go func(index int) {
			ready <- struct{}{}
			<-start
			ctx := context.WithValue(context.Background(), utils.CtxKeyLoginToken, tokens[index])
			principal := &utils.Principal{Kind: "user", UserID: fixture.ownerID, AuthVersion: 1, TokenExpiresAt: now.Add(time.Hour)}
			_, addErr := applications[index].AddInline(ctx, fixture.ownerID, manualIDs[index], manualservice.InlineItemInput{
				Kind: "text", Title: "跨实例用户配额竞争", Text: "12345", ClientRequestID: "database-user-byte-item-" + strconv.Itoa(index+1),
			}, principal)
			results <- addErr
		}(index)
	}
	for range applications {
		<-ready
	}
	close(start)

	successes, limited := 0, 0
	for range applications {
		switch addErr := <-results; {
		case addErr == nil:
			successes++
		case errors.Is(addErr, apperrors.ErrRateLimited):
			limited++
		default:
			t.Fatalf("unexpected cross-instance quota result: %v", addErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, limited)
	require.Equal(t, int64(1), countManualRows(t, fixture, "SELECT COUNT(*) FROM manual_items WHERE manual_id IN (?, ?)", manualIDs[0], manualIDs[1]))
	var totalBytes int64
	require.NoError(t, fixture.database.QueryRow("SELECT COALESCE(SUM(size_bytes), 0) FROM manual_items WHERE manual_id IN (?, ?)", manualIDs[0], manualIDs[1]).Scan(&totalBytes))
	require.Equal(t, int64(5), totalBytes)
}
