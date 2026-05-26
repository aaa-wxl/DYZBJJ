package service

import (
	"errors"
	"testing"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

func TestAuthLoginAndRequireBidder(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository())

	session, err := auth.Login("李四", auction.RoleBidder)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Token == "" {
		t.Fatal("Login() returned empty token")
	}
	if session.User.Name != "李四" {
		t.Fatalf("Login() user name = %q, want %q", session.User.Name, "李四")
	}
	if session.User.Role != auction.RoleBidder {
		t.Fatalf("Login() user role = %q, want %q", session.User.Role, auction.RoleBidder)
	}

	user, err := auth.Require(session.Token, auction.RoleBidder)
	if err != nil {
		t.Fatalf("Require() error = %v", err)
	}
	if user != session.User {
		t.Fatalf("Require() user = %+v, want %+v", user, session.User)
	}
}

func TestAuthRequireRejectsWrongRole(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository())
	session, err := auth.Login("李四", auction.RoleBidder)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if _, err := auth.Require(session.Token, auction.RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Require() error = %v, want %v", err, ErrForbidden)
	}
}

func TestAuthLoginRejectsEmptyName(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository())

	if _, err := auth.Login("  ", auction.RoleBidder); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Login(empty) error = %v, want %v", err, ErrUnauthorized)
	}
}

func TestAuthLoginRejectsInvalidRole(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository())

	if _, err := auth.Login("李四", auction.Role("GUEST")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Login(invalid role) error = %v, want %v", err, ErrForbidden)
	}
}
