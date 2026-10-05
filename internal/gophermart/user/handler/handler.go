// Package handler предоставляет HTTP-обработчики для регистрации новых пользователей,
// аутентификации существующих учетных записей и управления авторизационными куками.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/model"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/service"
)

// UserService определяет интерфейс взаимодействия со слоем бизнес-логики пользователей.
type UserService interface {
	// CreateUser регистрирует нового пользователя и возвращает сгенерированный JWT-токен.
	CreateUser(ctx context.Context, userLogin string, password string) (string, error)
	// LoginUser проверяет учетные данные и возвращает JWT-токен при успешном входе.
	LoginUser(ctx context.Context, userLogin string, password string) (string, error)
}

// Handler инкапсулирует структурированный логгер и интерфейс UserService для обработки входящих HTTP-запросов.
type Handler struct {
	logger  *slog.Logger
	service UserService
}

// New создает и инициализирует новый экземпляр Handler подсистемы управления пользователями.
func New(logger *slog.Logger, userService UserService) *Handler {
	return &Handler{
		logger:  logger,
		service: userService,
	}
}

// RegisterUser обрабатывает HTTP-запрос на регистрацию нового аккаунта.
// Декодирует пару логин/пароль из JSON-тела запроса и транслирует результаты бизнес-логики:
//   - 200 OK — пользователь успешно создан, авторизационный токен установлен в куку `jwt`.
//   - 400 Bad Request — невалидный JSON, пустые поля или наличие избыточных данных в теле запроса.
//   - 409 Conflict — переданный логин уже занят другим пользователем.
//   - 500 Internal Server Error — непредвиденная ошибка на стороне хранилища.
func (h *Handler) RegisterUser(writer http.ResponseWriter, request *http.Request) {

	var credentials model.Credentials

	if err := json.NewDecoder(request.Body).Decode(&credentials); err != nil {
		h.logger.Error(
			"failed to decode json into struct",
			"err", err,
		)
		http.Error(writer, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := credentials.Validate(); err != nil {
		h.logger.Info(
			"failed to validate credentials",
			"err", err,
		)
		http.Error(writer, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	jwtToken, err := h.service.CreateUser(request.Context(), credentials.Login, credentials.Password)
	if err != nil {
		h.writeServiceError(writer, err)
		return
	}
	h.setAuthCookie(writer, request, jwtToken)

	writer.WriteHeader(http.StatusOK)
}

// LoginUser обрабатывает HTTP-запрос на аутентификацию и вход существующего пользователя.
// Декодирует учетные данные из JSON-тела и сверяет их со слоем бизнес-логики:
//   - 200 OK — успешная аутентификация, обновленный авторизационный токен записан в куку `jwt`.
//   - 400 Bad Request — некорректная структура JSON-запроса или пустые поля.
//   - 401 Unauthorized — передан неверный логин или пароль.
//   - 500 Internal Server Error — внутренняя ошибка выполнения запроса к СУБД.
func (h *Handler) LoginUser(writer http.ResponseWriter, request *http.Request) {

	var credentials model.Credentials

	if err := json.NewDecoder(request.Body).Decode(&credentials); err != nil {
		h.logger.Error(
			"failed to decode json into struct",
			"err", err,
		)
		http.Error(writer, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if err := credentials.Validate(); err != nil {
		h.logger.Info(
			"failed to validate credentials",
			"err", err,
		)
		http.Error(writer, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	jwtToken, err := h.service.LoginUser(request.Context(), credentials.Login, credentials.Password)
	if err != nil {
		h.writeServiceError(writer, err)
		return
	}
	h.setAuthCookie(writer, request, jwtToken)

	writer.WriteHeader(http.StatusOK)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrUserAlreadyExists):
		h.logger.Info(
			"user already exists",
			"err", err,
		)
		status = http.StatusConflict
	case errors.Is(err, service.ErrInvalidCredentials):
		h.logger.Info(
			"invalid credentials",
			"err", err,
		)
		status = http.StatusUnauthorized
	default:
		h.logger.Error("user request failed", "err", err)
	}
	http.Error(w, http.StatusText(status), status)
}

func (h *Handler) setAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt",
		Path:     "/api/user",
		Value:    token,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}
