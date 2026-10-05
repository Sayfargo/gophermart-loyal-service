package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	authentication "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/auth"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/ctxkeys"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/http/middleware/mock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAuthMiddleware(t *testing.T) {
	userID := uuid.New()
	errUnavailable := errors.New("validator unavailable")
	tests := []struct {
		name           string
		cookie         *http.Cookie
		validatorErr   error
		wantStatus     int
		wantValidated  bool
		wantDownstream bool
	}{
		{name: "missing cookie", wantStatus: http.StatusUnauthorized},
		{name: "expired token", cookie: &http.Cookie{Name: "jwt", Value: "expired"}, validatorErr: fmt.Errorf("parse token: %w", authentication.ErrExpiredToken), wantStatus: http.StatusUnauthorized, wantValidated: true},
		{name: "invalid token", cookie: &http.Cookie{Name: "jwt", Value: "invalid"}, validatorErr: fmt.Errorf("parse token: %w", authentication.ErrInvalidToken), wantStatus: http.StatusUnauthorized, wantValidated: true},
		{name: "missing user id", cookie: &http.Cookie{Name: "jwt", Value: "missing-user-id"}, validatorErr: fmt.Errorf("parse token: %w", authentication.ErrMissingUserID), wantStatus: http.StatusUnauthorized, wantValidated: true},
		{name: "unexpected validator error", cookie: &http.Cookie{Name: "jwt", Value: "unavailable"}, validatorErr: errUnavailable, wantStatus: http.StatusInternalServerError, wantValidated: true},
		{name: "valid token", cookie: &http.Cookie{Name: "jwt", Value: "signed-token"}, wantStatus: http.StatusNoContent, wantValidated: true, wantDownstream: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downstream := false
			validator := mock.NewMockTokenValidator(gomock.NewController(t))
			if tt.wantValidated {
				validator.EXPECT().ValidateToken(tt.cookie.Value).Return(userID, tt.validatorErr)
			} else {
				validator.EXPECT().ValidateToken(gomock.Any()).Times(0)
			}
			middleware := AuthMiddleware(validator)
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				downstream = true
				gotID, err := ctxkeys.GetUserID(r.Context())
				require.NoError(t, err)
				require.Equal(t, userID, gotID)
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)

			require.Equal(t, tt.wantStatus, recorder.Code)
			require.Equal(t, tt.wantDownstream, downstream)
		})
	}
}
