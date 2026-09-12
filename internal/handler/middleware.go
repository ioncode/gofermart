package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
)

type contextKey string

const UserIDContextKey contextKey = "user_id"

// AuthMiddleware перехватывает запросы к защищенным ручкам,
// достает куку и просит сервис валидировать токен.
func (h *UserHandler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Извлекаем куку auth_token
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				http.Error(w, "Authentication cookie missing", http.StatusUnauthorized) // 401
				return
			}
			http.Error(w, "Failed to parse cookies", http.StatusBadRequest) // 400
			return
		}

		tokenString := cookie.Value
		if tokenString == "" {
			http.Error(w, "Token is empty", http.StatusUnauthorized) // 401
			return
		}

		// 2. Делегируем всю криптографию и валидацию сервису
		userID, err := h.service.ValidateToken(r.Context(), tokenString)
		if err != nil {
			log.Println(tokenString)
			log.Println(err)
			// Любая ошибка валидации токена означает сброс авторизации
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized) // 401
			return
		}

		// 3. Обогащаем контекст и передаем управление дальше по цепочке
		ctx := context.WithValue(r.Context(), UserIDContextKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
