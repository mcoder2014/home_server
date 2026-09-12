package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebProjectEnumsKeepDatabaseIntegersAndJSONStringValues(t *testing.T) {
	tests := []struct {
		name        string
		value       interface{}
		stringValue string
		intValue    int
	}{
		{name: "owner access", value: WebProjectAccessOwner, stringValue: "owner", intValue: 1},
		{name: "public access", value: WebProjectAccessPublic, stringValue: "public", intValue: 4},
		{name: "draft project", value: WebProjectStatusDraft, stringValue: "draft", intValue: 1},
		{name: "deleted project", value: WebProjectStatusDeleted, stringValue: "deleted", intValue: 4},
		{name: "ready release", value: WebProjectReleaseReady, stringValue: "ready", intValue: 2},
		{name: "deleting release", value: WebProjectReleaseDeleting, stringValue: "deleting", intValue: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.intValue, intValue(tt.value))
			encoded, err := json.Marshal(tt.value)
			require.NoError(t, err)
			require.JSONEq(t, `"`+tt.stringValue+`"`, string(encoded))
		})
	}
}

func TestParseWebProjectEnumsRejectsUnknownStrings(t *testing.T) {
	access, ok := ParseWebProjectAccess("members")
	require.True(t, ok)
	require.Equal(t, WebProjectAccessMembers, access)
	_, ok = ParseWebProjectAccess("unknown")
	require.False(t, ok)

	status, ok := ParseWebProjectStatus("enabled")
	require.True(t, ok)
	require.Equal(t, WebProjectStatusEnabled, status)
	_, ok = ParseWebProjectStatus("unknown")
	require.False(t, ok)

	releaseStatus, ok := ParseWebProjectReleaseStatus("failed")
	require.True(t, ok)
	require.Equal(t, WebProjectReleaseFailed, releaseStatus)
	_, ok = ParseWebProjectReleaseStatus("unknown")
	require.False(t, ok)
}

func TestWebProjectReleaseExtraKeepsErrorMessageAPICompatibility(t *testing.T) {
	release := WebProjectRelease{Extra: `{"error_message":"archive checksum mismatch","future_key":{"enabled":true}}`}
	require.NoError(t, release.DecodeExtra())
	require.Equal(t, "archive checksum mismatch", release.ErrorMessage)

	release.ErrorMessage = strings.Repeat("x", 2048)
	require.NoError(t, release.EncodeExtra())
	require.JSONEq(t, `{"error_message":"`+strings.Repeat("x", 2048)+`","future_key":{"enabled":true}}`, release.Extra)
}

func TestWebProjectReleaseExtraNormalizesJSONNullBeforeWriting(t *testing.T) {
	release := WebProjectRelease{Extra: "null", ErrorMessage: "failed"}
	require.NotPanics(t, func() {
		require.NoError(t, release.EncodeExtra())
	})
	require.JSONEq(t, `{"error_message":"failed"}`, release.Extra)
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case WebProjectAccess:
		return int(typed)
	case WebProjectStatus:
		return int(typed)
	case WebProjectReleaseStatus:
		return int(typed)
	default:
		return 0
	}
}
