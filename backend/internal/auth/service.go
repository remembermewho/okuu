package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"okuu/internal/db/sqlc"
)

// Роли сотрудника в центре.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleTeacher = "teacher"
)

const (
	// SessionTTL — срок жизни сессии и cookie.
	SessionTTL = 30 * 24 * time.Hour
	// SessionTouchEvery — продлеваем сессию при активности не чаще этого интервала.
	SessionTouchEvery = 24 * time.Hour

	// LoginMaxFails неудачных входов за LoginWindow — и вход блокируется (на телефон и на IP).
	LoginMaxFails = 5
	LoginWindow   = 15 * time.Minute

	tokenBytes      = 32
	maxNameLen      = 100
	maxUserAgentLen = 512
)

// Options — необязательные настройки сервиса.
type Options struct {
	Now          func() time.Time // по умолчанию time.Now
	Limiter      *Limiter         // по умолчанию LoginMaxFails за LoginWindow
	CookieSecure bool             // Secure-флаг cookie (prod)
	Logger       *slog.Logger
}

// Service — регистрация, вход и сессии сотрудников.
type Service struct {
	pool         *pgxpool.Pool
	q            *sqlc.Queries
	now          func() time.Time
	limiter      *Limiter
	cookieSecure bool
	logger       *slog.Logger
}

// NewService создаёт сервис авторизации.
func NewService(pool *pgxpool.Pool, opt Options) *Service {
	s := &Service{
		pool:         pool,
		q:            sqlc.New(pool),
		now:          opt.Now,
		limiter:      opt.Limiter,
		cookieSecure: opt.CookieSecure,
		logger:       opt.Logger,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.limiter == nil {
		s.limiter = NewLimiter(LoginMaxFails, LoginWindow, s.now)
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	return s
}

// Session — выданная сессия. Token уходит только в cookie.
type Session struct {
	Token     string
	UserID    uuid.UUID
	ExpiresAt time.Time
}

// RegisterInput — данные регистрации владельца и центра.
type RegisterInput struct {
	Phone      string
	Password   string
	Name       string
	CenterName string
	UserAgent  string
}

// Register создаёт пользователя, центр и членство owner в одной транзакции и сразу открывает сессию.
func (s *Service) Register(ctx context.Context, in RegisterInput) (Session, error) {
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return Session{}, err
	}
	name, err := requiredName("name", "Имя", in.Name)
	if err != nil {
		return Session{}, err
	}
	centerName, err := requiredName("center_name", "Название центра", in.CenterName)
	if err != nil {
		return Session{}, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return Session{}, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return Session{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{Phone: phone, Name: name, PasswordHash: hash})
	if isUniqueViolation(err) {
		return Session{}, ErrPhoneTaken
	}
	if err != nil {
		return Session{}, fmt.Errorf("создание пользователя: %w", err)
	}
	// Телефон центра по умолчанию — телефон владельца; меняется в настройках.
	center, err := q.CreateCenter(ctx, sqlc.CreateCenterParams{Name: centerName, Phone: &phone})
	if err != nil {
		return Session{}, fmt.Errorf("создание центра: %w", err)
	}
	if _, err := q.CreateMembership(ctx, sqlc.CreateMembershipParams{
		UserID: user.ID, CenterID: center.ID, Role: RoleOwner,
	}); err != nil {
		return Session{}, fmt.Errorf("создание членства: %w", err)
	}
	sess, err := s.createSession(ctx, q, user.ID, in.UserAgent)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// LoginInput — данные входа; IP нужен для лимита попыток.
type LoginInput struct {
	Phone     string
	Password  string
	UserAgent string
	IP        string
}

// Login проверяет телефон и пароль и открывает сессию. Неверный телефон и неверный
// пароль дают одну и ту же ErrInvalidCredentials.
func (s *Service) Login(ctx context.Context, in LoginInput) (Session, error) {
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return Session{}, err
	}
	phoneKey, ipKey := "phone:"+phone, "ip:"+in.IP
	if blocked, retry := s.limiter.Blocked(phoneKey, ipKey); blocked {
		return Session{}, &TooManyAttemptsError{RetryAfter: retry}
	}

	if _, err := DeleteExpiredSessions(ctx, s.pool); err != nil {
		return Session{}, fmt.Errorf("очистка сессий: %w", err)
	}

	user, err := s.q.GetUserByPhone(ctx, phone)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, err
	}
	hash := user.PasswordHash
	if !found {
		hash = dummyHash() // то же время ответа, что и для существующего номера
	}
	ok, err := VerifyPassword(in.Password, hash)
	if err != nil {
		return Session{}, fmt.Errorf("проверка пароля пользователя %s: %w", user.ID, err)
	}
	if !found || !ok {
		s.limiter.Fail(phoneKey, ipKey)
		return Session{}, ErrInvalidCredentials
	}

	s.limiter.Reset(phoneKey)
	return s.createSession(ctx, s.q, user.ID, in.UserAgent)
}

// Logout удаляет сессию по токену из cookie. Пустой или неизвестный токен — не ошибка.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.q.DeleteSession(ctx, hashToken(token))
}

// Authenticate находит сессию по токену. Если с последнего продления прошли сутки,
// продлевает её на SessionTTL и возвращает extended=true — тогда нужно обновить cookie.
func (s *Service) Authenticate(ctx context.Context, token string) (sess Session, extended bool, err error) {
	if token == "" {
		return Session{}, false, ErrNoSession
	}
	row, err := s.q.GetSessionByTokenHash(ctx, hashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, ErrNoSession
	}
	if err != nil {
		return Session{}, false, err
	}

	sess = Session{Token: token, UserID: row.UserID, ExpiresAt: row.ExpiresAt}
	last := row.CreatedAt
	if row.LastSeenAt != nil {
		last = *row.LastSeenAt
	}
	now := s.now()
	if now.Sub(last) < SessionTouchEvery {
		return sess, false, nil
	}
	sess.ExpiresAt = now.Add(SessionTTL)
	if err := s.q.TouchSession(ctx, sqlc.TouchSessionParams{
		TokenHash: row.TokenHash, LastSeenAt: &now, ExpiresAt: sess.ExpiresAt,
	}); err != nil {
		return Session{}, false, err
	}
	return sess, true, nil
}

// Profile — пользователь и его центры для GET /me.
type Profile struct {
	User    sqlc.User
	Centers []sqlc.ListMembershipsByUserRow
}

// Profile загружает пользователя и список его центров.
func (s *Service) Profile(ctx context.Context, userID uuid.UUID) (Profile, error) {
	user, err := s.q.GetUserByID(ctx, userID)
	if err != nil {
		return Profile{}, err
	}
	centers, err := s.q.ListMembershipsByUser(ctx, userID)
	if err != nil {
		return Profile{}, err
	}
	return Profile{User: user, Centers: centers}, nil
}

func (s *Service) createSession(ctx context.Context, q *sqlc.Queries, userID uuid.UUID, userAgent string) (Session, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	expires := s.now().Add(SessionTTL)

	var ua *string
	if userAgent != "" {
		ua = &userAgent
		if len(userAgent) > maxUserAgentLen {
			truncated := strings.ToValidUTF8(userAgent[:maxUserAgentLen], "")
			ua = &truncated
		}
	}
	if _, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		TokenHash: hashToken(token), UserID: userID, ExpiresAt: expires, UserAgent: ua,
	}); err != nil {
		return Session{}, fmt.Errorf("создание сессии: %w", err)
	}
	return Session{Token: token, UserID: userID, ExpiresAt: expires}, nil
}

// hashToken — sha256 от токена; в БД хранится только он.
func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func requiredName(field, label, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", &ValidationError{Field: field, Message: label + " — обязательное поле"}
	}
	// encoding/json заменяет байты не в UTF-8 на U+FFFD — не сохраняем такой мусор.
	if strings.ContainsRune(v, utf8.RuneError) {
		return "", &ValidationError{Field: field, Message: label + " — некорректная кодировка, нужен UTF-8"}
	}
	if utf8.RuneCountInString(v) > maxNameLen {
		return "", &ValidationError{Field: field, Message: fmt.Sprintf("%s — не длиннее %d символов", label, maxNameLen)}
	}
	return v, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
