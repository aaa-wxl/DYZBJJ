package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")
var ErrAuthStorage = errors.New("auth storage failed")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrUserDisabled = errors.New("user disabled")
var ErrTokenExpired = errors.New("token expired")
var ErrInvalidToken = errors.New("invalid token")

type AuthService struct {
	repo      repository.AuctionRepository
	jwtSecret []byte
	jwtTTL    time.Duration
}

type LoginSession struct {
	Token string       `json:"token"`
	User  auction.User `json:"user"`
}

type demoUserSeed struct {
	ID          string
	Username    string
	Password    string
	DisplayName string
	Role        auction.Role
}

type jwtClaims struct {
	Subject  string       `json:"sub"`
	Username string       `json:"username"`
	Role     auction.Role `json:"role"`
	IssuedAt int64        `json:"iat"`
	Expires  int64        `json:"exp"`
}

var demoUsers = []demoUserSeed{
	{ID: "usr-admin", Username: "admin", Password: "admin123", DisplayName: "管理员", Role: auction.RoleAdmin},
	{ID: "usr-user-a", Username: "userA", Password: "123456", DisplayName: "用户A", Role: auction.RoleBidder},
	{ID: "usr-user-b", Username: "userB", Password: "123456", DisplayName: "用户B", Role: auction.RoleBidder},
	{ID: "usr-user-c", Username: "userC", Password: "123456", DisplayName: "用户C", Role: auction.RoleBidder},
}

func NewAuthService(repo repository.AuctionRepository, jwtSecret string, jwtTTL time.Duration) *AuthService {
	if jwtSecret == "" {
		jwtSecret = "local-demo-jwt-secret"
	}
	if jwtTTL == 0 {
		jwtTTL = 24 * time.Hour
	}
	return &AuthService{repo: repo, jwtSecret: []byte(jwtSecret), jwtTTL: jwtTTL}
}

func (s *AuthService) SeedDemoUsers() error {
	now := time.Now().UTC()
	for _, seed := range demoUsers {
		salt := "demo:" + seed.Username
		user := auction.User{
			ID:           seed.ID,
			Username:     seed.Username,
			DisplayName:  seed.DisplayName,
			PasswordSalt: salt,
			PasswordHash: hashPassword(salt, seed.Password),
			Role:         seed.Role,
			Status:       auction.UserActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if existing, err := s.repo.GetUser(seed.ID); err == nil && !existing.CreatedAt.IsZero() {
			user.CreatedAt = existing.CreatedAt
		}
		if err := s.repo.UpsertUser(user); err != nil {
			return ErrAuthStorage
		}
	}
	return nil
}

func (s *AuthService) Login(username, password string) (LoginSession, error) {
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return LoginSession{}, ErrInvalidCredentials
	}
	user, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return LoginSession{}, ErrInvalidCredentials
	}
	if user.Status != auction.UserActive {
		return LoginSession{}, ErrUserDisabled
	}
	if user.PasswordHash != hashPassword(user.PasswordSalt, password) {
		return LoginSession{}, ErrInvalidCredentials
	}
	token, err := s.signJWT(user, time.Now().UTC())
	if err != nil {
		return LoginSession{}, err
	}
	return LoginSession{Token: token, User: user}, nil
}

func (s *AuthService) Require(token string, role auction.Role) (auction.User, error) {
	claims, err := s.parseJWT(strings.TrimSpace(token), time.Now().UTC())
	if err != nil {
		return auction.User{}, err
	}
	user, err := s.repo.GetUser(claims.Subject)
	if err != nil {
		return auction.User{}, ErrUnauthorized
	}
	if user.Status != auction.UserActive {
		return auction.User{}, ErrUserDisabled
	}
	if user.Role != role {
		return auction.User{}, ErrForbidden
	}
	return user, nil
}

func hashPassword(salt, password string) string {
	sum := sha256.Sum256([]byte(salt + password))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) signJWT(user auction.User, now time.Time) (string, error) {
	claims := jwtClaims{
		Subject:  user.ID,
		Username: user.Username,
		Role:     user.Role,
		IssuedAt: now.Unix(),
		Expires:  now.Add(s.jwtTTL).Unix(),
	}
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	return unsigned + "." + s.jwtSignature(unsigned), nil
}

func (s *AuthService) parseJWT(token string, now time.Time) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, ErrInvalidToken
	}
	unsigned := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(s.jwtSignature(unsigned))) {
		return jwtClaims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, ErrInvalidToken
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}, ErrInvalidToken
	}
	if claims.Expires <= now.Unix() {
		return jwtClaims{}, ErrTokenExpired
	}
	if claims.Subject == "" {
		return jwtClaims{}, ErrInvalidToken
	}
	return claims, nil
}

func (s *AuthService) jwtSignature(unsigned string) string {
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
