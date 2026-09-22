package postgres_test

import (
	"context"
	"testing"

	"github.com/ioncode/gofermart/internal/repository"
	pgrepo "github.com/ioncode/gofermart/internal/repository/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type UserRepoTestSuite struct {
	BasePostgresTestSuite // Переиспользуем общую фикстуру базы данных и миграций
	repo                  *pgrepo.UserRepository
}

func (s *UserRepoTestSuite) SetupTest() {
	s.repo = pgrepo.NewUserRepository(s.Pool)
}

// 1. Тест успешного создания пользователя и перехвата ошибки дублирования логина
func (s *UserRepoTestSuite) TestCreateUser_SuccessAndDuplicate() {
	ctx := context.Background()
	login := "unique_go_developer"
	passwordHash := "hashed_super_secure_password"

	// Сценарий А: Успешное создание нового пользователя
	userID, err := s.repo.CreateUser(ctx, login, passwordHash)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), userID) // База данных должна вернуть сгенерированный UUID

	// Проверяем запись в БД напрямую через SQL
	var dbHash string
	err = s.Pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", userID).Scan(&dbHash)
	s.Require().NoError(err)
	assert.Equal(s.T(), passwordHash, dbHash)

	// Сценарий Б: Попытка зарегистрировать пользователя с тем же логином
	// Тест проверяет, что перехват pgErr.Code == "23505" возвращает ErrDuplicateLogin
	_, err = s.repo.CreateUser(ctx, login, "another_hash")
	assert.ErrorIs(s.T(), err, repository.ErrDuplicateLogin)
}

// 2. Тест проверки существования логина в системе
func (s *UserRepoTestSuite) TestCheckUserExists() {
	ctx := context.Background()
	login := "existing_user"

	// Проверяем, что изначально пользователя нет
	exists, err := s.repo.CheckUserExists(ctx, login)
	assert.NoError(s.T(), err)
	assert.False(s.T(), exists)

	// Создаем пользователя через репозиторий
	_, err = s.repo.CreateUser(ctx, login, "hash")
	s.Require().NoError(err)

	// Проверяем теперь — метод должен вернуть true
	exists, err = s.repo.CheckUserExists(ctx, login)
	assert.NoError(s.T(), err)
	assert.True(s.T(), exists)
}

// 3. Тест получения ID и хэша пароля по логину
func (s *UserRepoTestSuite) TestGetPasswordHash_SuccessAndNotFound() {
	ctx := context.Background()
	login := "auth_user"
	passwordHash := "secret_token_hash"

	// Проверяем сценарий, когда пользователя нет — должен вернуть ErrUserNotFound
	_, _, err := s.repo.GetPasswordHash(ctx, login)
	assert.ErrorIs(s.T(), err, repository.ErrUserNotFound)

	// Создаем пользователя
	expectedID, err := s.repo.CreateUser(ctx, login, passwordHash)
	s.Require().NoError(err)

	// Успешный сценарий авторизации: получаем ID и хэш
	dbID, dbHash, err := s.repo.GetPasswordHash(ctx, login)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), expectedID, dbID)
	assert.Equal(s.T(), passwordHash, dbHash)
}

func TestUserRepository(t *testing.T) {
	suite.Run(t, new(UserRepoTestSuite))
}
