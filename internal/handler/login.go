package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ioncode/gofermart/internal/service"
)

// Login обрабатывает входящий HTTP-запрос POST /api/user/login на аутентификацию пользователя.
// Метод использует пул объектов registerRequestPool для исключения утечек в кучу и возвращает стандартные HTTP-статусы (200, 400, 401, 500).
func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	req := registerRequestPool.Get().(*RegisterRequest)
	defer func() {
		req.Reset()
		registerRequestPool.Put(req)
	}()

	if !ReadJSONOptimized(w, r, req) {
		h.logger.Debug("Ошибка чтения тела запроса на регистрацию")
		return
	}

	req.Login = strings.TrimSpace(req.Login)
	req.Password = strings.TrimSpace(req.Password)
	if req.Login == "" || req.Password == "" {
		http.Error(w, "Login and password are required", http.StatusBadRequest)
		return
	}

	token, err := h.service.Authenticate(r.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			http.Error(w, "Invalid login or password", http.StatusUnauthorized)
			return
		}
		h.logger.Error("Authentication processing failed", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	ttl := h.service.TokenTTL()
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   h.isProd,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusOK)
}
