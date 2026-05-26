package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

var ErrUnauthorized = errors.New("unauthorized")
var ErrForbidden = errors.New("forbidden")
var ErrAuthStorage = errors.New("auth storage failed")

type AuthService struct {
	repo repository.AuctionRepository
}

type LoginSession struct {
	Token string       `json:"token"`
	User  auction.User `json:"user"`
}

func NewAuthService(repo repository.AuctionRepository) *AuthService {
	return &AuthService{repo: repo}
}

func (s *AuthService) Login(name string, role auction.Role) (LoginSession, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return LoginSession{}, ErrUnauthorized
	}
	if !validRole(role) {
		return LoginSession{}, ErrForbidden
	}

	now := time.Now().UTC()
	token, err := newSessionToken()
	if err != nil {
		return LoginSession{}, err
	}
	user := auction.User{
		ID:        auction.NewID("usr"),
		Name:      name,
		Role:      role,
		CreatedAt: now,
	}
	session := auction.Session{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	if err := s.repo.SaveUser(user); err != nil {
		return LoginSession{}, ErrAuthStorage
	}
	if err := s.repo.SaveSession(session); err != nil {
		return LoginSession{}, ErrAuthStorage
	}
	return LoginSession{Token: token, User: user}, nil
}

func (s *AuthService) Require(token string, role auction.Role) (auction.User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return auction.User{}, ErrUnauthorized
	}
	user, err := s.repo.GetUserByToken(token)
	if err != nil {
		return auction.User{}, ErrUnauthorized
	}
	if user.Role != role {
		return auction.User{}, ErrForbidden
	}
	return user, nil
}

func validRole(role auction.Role) bool {
	return role == auction.RoleAdmin || role == auction.RoleBidder
}

func newSessionToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
