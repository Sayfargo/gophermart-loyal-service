package handler

import (
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var tlsState = tls.ConnectionState{}

func setupHandler(t *testing.T) (*Handler, *MockUserService) {
	userService := NewMockUserService(t)

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	return New(logger, userService), userService
}

func TestHandler_RegisterUser(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCalled bool
		wantSecure bool
		requestTLS bool
	}{
		{name: "malformed json", body: `{"login":`, wantStatus: http.StatusBadRequest},
		{name: "empty login", body: `{"login":"","password":"secret"}`, wantStatus: http.StatusBadRequest},
		{name: "empty password", body: `{"login":"alice","password":""}`, wantStatus: http.StatusBadRequest},
		{name: "duplicate user", body: `{"login":"alice","password":"secret"}`, serviceErr: service.ErrUserAlreadyExists, wantStatus: http.StatusConflict, wantCalled: true},
		{name: "internal error", body: `{"login":"alice","password":"secret"}`, serviceErr: errors.New("storage failed"), wantStatus: http.StatusInternalServerError, wantCalled: true},
		{name: "success over http", body: `{"login":"alice","password":"secret"}`, wantStatus: http.StatusOK, wantCalled: true},
		{name: "success over https", body: `{"login":"alice","password":"secret"}`, wantStatus: http.StatusOK, wantCalled: true, wantSecure: true, requestTLS: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceMock := NewMockUserService(t)
			if tt.wantCalled {
				serviceMock.EXPECT().
					CreateUser(mock.Anything, "alice", "secret").
					Return("signed-token", tt.serviceErr)
			}
			h := New(slog.New(slog.NewTextHandler(io.Discard, nil)), serviceMock)

			req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(tt.body))
			if tt.requestTLS {
				req.URL.Scheme = "https"
				req.TLS = &tlsState
			}
			recorder := httptest.NewRecorder()

			h.RegisterUser(recorder, req)

			require.Equal(t, tt.wantStatus, recorder.Code)
			if tt.wantStatus == http.StatusOK {
				cookies := recorder.Result().Cookies()
				require.Len(t, cookies, 1)
				require.Equal(t, "jwt", cookies[0].Name)
				require.Equal(t, "signed-token", cookies[0].Value)
				require.Equal(t, "/api/user", cookies[0].Path)
				require.True(t, cookies[0].HttpOnly)
				require.Equal(t, tt.wantSecure, cookies[0].Secure)
			}
		})
	}
}

func TestHandler_LoginUser(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{name: "malformed json", body: `{"login":`, wantStatus: http.StatusBadRequest},
		{name: "empty login", body: `{"login":"","password":"secret"}`, wantStatus: http.StatusBadRequest},
		{name: "empty password", body: `{"login":"alice","password":""}`, wantStatus: http.StatusBadRequest},
		{name: "invalid credentials", body: `{"login":"alice","password":"secret"}`, serviceErr: service.ErrInvalidCredentials, wantStatus: http.StatusUnauthorized, wantCalled: true},
		{name: "internal error", body: `{"login":"alice","password":"secret"}`, serviceErr: errors.New("storage failed"), wantStatus: http.StatusInternalServerError, wantCalled: true},
		{name: "success", body: `{"login":"alice","password":"secret"}`, wantStatus: http.StatusOK, wantCalled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceMock := NewMockUserService(t)
			if tt.wantCalled {
				serviceMock.EXPECT().
					LoginUser(mock.Anything, "alice", "secret").
					Return("signed-token", tt.serviceErr)
			}
			h := New(slog.New(slog.NewTextHandler(io.Discard, nil)), serviceMock)

			recorder := httptest.NewRecorder()
			h.LoginUser(recorder, httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(tt.body)))

			require.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func TestHandler_RegisterUserTable(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setupMock  func(*MockUserService)
		wantStatus int
		wantCookie bool
		wantSecure bool
		requestTLS bool
	}{
		{
			name:       "invalid json",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty login",
			body:       `{"login":"","password":"password"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty password",
			body:       `{"login":"login","password":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "user already exists",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					CreateUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("", service.ErrUserAlreadyExists)
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "internal service error",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					CreateUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("", errors.New("database error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					CreateUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("jwt-token", nil)
			},
			wantStatus: http.StatusOK,
			wantCookie: true,
		},
		{
			name: "success https",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					CreateUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("jwt-token", nil)
			},
			wantStatus: http.StatusOK,
			wantCookie: true,
			requestTLS: true,
			wantSecure: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, serviceMock := setupHandler(t)

			if tt.setupMock != nil {
				tt.setupMock(serviceMock)
			}

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/user/register",
				strings.NewReader(tt.body),
			)

			if tt.requestTLS {
				req.TLS = &tls.ConnectionState{}
			}

			recorder := httptest.NewRecorder()

			h.RegisterUser(recorder, req)

			require.Equal(t, tt.wantStatus, recorder.Code)

			if tt.wantCookie {
				cookies := recorder.Result().Cookies()

				require.Len(t, cookies, 1)

				cookie := cookies[0]

				require.Equal(t, "jwt", cookie.Name)
				require.Equal(t, "jwt-token", cookie.Value)
				require.Equal(t, "/api/user", cookie.Path)
				require.True(t, cookie.HttpOnly)
				require.Equal(t, tt.wantSecure, cookie.Secure)
			}
		})
	}
}

func TestHandler_LoginUserTable(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		setupMock  func(*MockUserService)
		wantStatus int
		wantCookie bool
	}{
		{
			name:       "invalid json",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty login",
			body:       `{"login":"","password":"password"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty password",
			body:       `{"login":"login","password":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid credentials",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					LoginUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("", service.ErrInvalidCredentials)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "internal error",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					LoginUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("", errors.New("database error"))
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success",
			body: `{"login":"login","password":"password"}`,
			setupMock: func(m *MockUserService) {
				m.EXPECT().
					LoginUser(
						mock.Anything,
						"login",
						"password",
					).
					Return("jwt-token", nil)
			},
			wantStatus: http.StatusOK,
			wantCookie: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, serviceMock := setupHandler(t)

			if tt.setupMock != nil {
				tt.setupMock(serviceMock)
			}

			req := httptest.NewRequest(
				http.MethodPost,
				"/api/user/login",
				strings.NewReader(tt.body),
			)

			recorder := httptest.NewRecorder()

			h.LoginUser(recorder, req)

			require.Equal(t, tt.wantStatus, recorder.Code)

			if tt.wantCookie {
				cookies := recorder.Result().Cookies()

				require.Len(t, cookies, 1)
				require.Equal(t, "jwt-token", cookies[0].Value)
			}
		})
	}
}
