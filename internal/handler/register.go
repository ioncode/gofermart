package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ioncode/gofermart/internal/service"
)

// RegisterRequest представляет DTO входящего запроса регистрации
type RegisterRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (h *UserHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	// Читаем JSON. Если формат битый -> 400 Bad Request
	if !ReadJSONOptimized(w, r, &req) {
		return
	}

	// Базовая валидация на пустые поля -> 400 Bad Request
	req.Login = strings.TrimSpace(req.Login)
	req.Password = strings.TrimSpace(req.Password)
	if req.Login == "" || req.Password == "" {
		http.Error(w, "Login and password are required", http.StatusBadRequest)
		return
	}

	// Передаем управление в слой бизнес-логики (прокидываем r.Context())
	// Сервис возвращает сгенерированный токен (JWT или сессию)
	token, err := h.service.Register(r.Context(), req.Login, req.Password)
	if err != nil {
		// Если логин уже занят -> 409 Conflict
		if errors.Is(err, service.ErrLoginConflict) {
			http.Error(w, "Login already taken", http.StatusConflict) // 409
			return
		}
		// Любая другая непредвиденная ошибка (ошибка БД и т.д.) -> 500
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Автоматическая аутентификация: записываем токен в куку
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,  // Защита от XSS
		Secure:   false, // Только для HTTPS
		SameSite: http.SameSiteLaxMode,
	})

	// Успешный ответ -> 200 OK
	w.WriteHeader(http.StatusOK)
}
