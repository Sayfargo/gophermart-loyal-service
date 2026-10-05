package middleware

import (
	"errors"
	"net/http"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/transport/ctxkeys"

	authentication "github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/auth"
	"github.com/google/uuid"
)

//go:generate go tool mockgen -source=auth.go -destination=mock/token_validator_gen.go -package=mock

type TokenValidator interface {
	ValidateToken(tokenString string) (uuid.UUID, error)
}

func AuthMiddleware(tokenValidator TokenValidator) func(http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			jwtCookie, err := request.Cookie("jwt")
			if err != nil {
				http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			userID, err := tokenValidator.ValidateToken(jwtCookie.Value)
			if err != nil {
				switch {
				case errors.Is(err, authentication.ErrExpiredToken),
					errors.Is(err, authentication.ErrInvalidToken),
					errors.Is(err, authentication.ErrMissingUserID):
					http.Error(writer, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				default:
					http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				return
			}
			request = request.WithContext(ctxkeys.WithUserID(request.Context(), userID))
			handler.ServeHTTP(writer, request)
		})
	}
}
