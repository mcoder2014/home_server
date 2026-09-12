package ginfmt

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	apperrors "github.com/mcoder2014/home_server/errors"
	"github.com/stretchr/testify/require"
)

func TestLegacyWrappedErrorsKeepTheirCodes(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	FormatWithError(c, fmt.Errorf("internal detail: %w", apperrors.New(apperrors.ErrorCodeUserNotLogin)))
	require.Equal(t, 200, recorder.Code)
	var body BaseResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, int(apperrors.ErrorCodeUserNotLogin), body.Code)
	require.NotContains(t, body.Message, "internal detail")
}

func TestAPIFailurePreservesCategoryWithoutLeakingCause(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	Fail(c, fmt.Errorf("private-file-or-credential: %w", apperrors.ErrForbidden))
	require.Equal(t, 403, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "private-file-or-credential")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}
