package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/user/model"
	"github.com/Sayfargo/gophermart-loyal-service/internal/testenv"
	gophermartMigrations "github.com/Sayfargo/gophermart-loyal-service/migrations/gophermart"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	env := testenv.Setup(ctx, gophermartMigrations.EmbedMigrations)
	testPool = env.Pool

	code := m.Run()
	env.Close(ctx)
	os.Exit(code)
}

func cleanDB(t *testing.T) {
	t.Helper()

	_, err := testPool.Exec(context.Background(), `TRUNCATE users RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

func newRepo(t *testing.T) *UserRepository {
	t.Helper()

	return New(testPool)
}

// CreateUser
func TestUserRepository_SaveUserInfo(t *testing.T) {
	t.Cleanup(func() {
		cleanDB(t)
	})

	repo := newRepo(t)

	user := model.UserInfo{
		UUID:         uuid.New(),
		Login:        "test_login",
		PasswordHash: "password_hash",
		CreatedAt:    time.Now().UTC(),
	}

	err := repo.SaveUserInfo(context.Background(), user)
	require.NoError(t, err)

	var got model.UserInfo

	err = testPool.QueryRow(
		context.Background(),
		`SELECT id, login, password_hash, created_at
		 FROM users
		 WHERE id = $1`,
		user.UUID,
	).Scan(
		&got.UUID,
		&got.Login,
		&got.PasswordHash,
		&got.CreatedAt,
	)

	require.NoError(t, err)

	assert.Equal(t, user.UUID, got.UUID)
	assert.Equal(t, user.Login, got.Login)
	assert.Equal(t, user.PasswordHash, got.PasswordHash)
	assert.WithinDuration(t, user.CreatedAt, got.CreatedAt, time.Second)

	var (
		balanceUser uuid.UUID
		current     decimal.Decimal
		withdrawn   decimal.Decimal
	)

	err = testPool.QueryRow(
		context.Background(),
		`SELECT user_uuid, current, withdrawn
		 FROM balance
		 WHERE user_uuid = $1`,
		user.UUID,
	).Scan(
		&balanceUser,
		&current,
		&withdrawn,
	)

	require.NoError(t, err)

	assert.Equal(t, user.UUID, balanceUser)
	assert.True(t, current.Equal(decimal.Zero))
	assert.True(t, withdrawn.Equal(decimal.Zero))
}

func TestUserRepository_SaveUserInfo_UserAlreadyExists_Rollback(t *testing.T) {
	t.Cleanup(func() {
		cleanDB(t)
	})

	repo := newRepo(t)
	ctx := context.Background()

	user := model.UserInfo{
		UUID:         uuid.New(),
		Login:        "duplicate_login",
		PasswordHash: "hash",
		CreatedAt:    time.Now().UTC(),
	}

	err := repo.SaveUserInfo(ctx, user)
	require.NoError(t, err)

	anotherUser := model.UserInfo{
		UUID:         uuid.New(),
		Login:        user.Login,
		PasswordHash: "another_hash",
		CreatedAt:    time.Now().UTC(),
	}

	err = repo.SaveUserInfo(ctx, anotherUser)
	require.ErrorIs(t, err, ErrUserAlreadyExists)

	var count int

	err = testPool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM balance WHERE user_uuid = $1`,
		anotherUser.UUID,
	).Scan(&count)

	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// FindUserInfo
func TestFindUserInfo_Success(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := newRepo(t)

	var (
		expectedUUID         = uuid.New()
		expectedLogin        = "test-login"
		expectedPasswordHash = "test-password-hash"
		expectedCreatedAt    = time.Now().UTC()
	)

	query := `
		INSERT INTO users(id, login, password_hash, created_at)
		VALUES ($1,$2, $3, $4)
	`

	_, err := testPool.Exec(
		ctx,
		query,
		expectedUUID,
		expectedLogin,
		expectedPasswordHash,
		expectedCreatedAt,
	)
	require.NoError(t, err)

	result, err := repo.FindUserInfo(ctx, expectedLogin)
	require.NoError(t, err)

	assert.Equal(t, expectedUUID, result.UUID)
	assert.Equal(t, expectedLogin, result.Login)
	assert.Equal(t, expectedPasswordHash, result.PasswordHash)

	assert.WithinDuration(t, expectedCreatedAt, result.CreatedAt, time.Second)
}

func TestFindUserInfo_UserNotFound(t *testing.T) {
	cleanDB(t)
	ctx := context.Background()
	repo := newRepo(t)

	result, err := repo.FindUserInfo(ctx, "non-exist")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUserNotFound)

	require.Nil(t, result)
}
