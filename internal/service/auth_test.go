package service

import (
	"errors"
	"strings"
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

func TestAuthKeepsSeparateUsersForFastLogins(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository())
	admin, err := auth.Login("admin", auction.RoleAdmin)
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	bidder, err := auth.Login("bidder", auction.RoleBidder)
	if err != nil {
		t.Fatalf("bidder login: %v", err)
	}
	if admin.User.ID == bidder.User.ID {
		t.Fatalf("users share id %q", admin.User.ID)
	}
	user, err := auth.Require(admin.Token, auction.RoleAdmin)
	if err != nil {
		t.Fatalf("require admin: %v", err)
	}
	if user.Role != auction.RoleAdmin {
		t.Fatalf("admin token resolved to role %s", user.Role)
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

func TestAuthLoginHidesStorageErrors(t *testing.T) {
	const sensitivePath = `C:\secret\auth.json`

	tests := []struct {
		name     string
		saveUser error
		saveSess error
	}{
		{name: "save user", saveUser: errors.New("open " + sensitivePath + ": access denied")},
		{name: "save session", saveSess: errors.New("write " + sensitivePath + ": access denied")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewAuthService(&authRepoStub{
				saveUserErr:    tt.saveUser,
				saveSessionErr: tt.saveSess,
			})

			_, err := auth.Login("alice", auction.RoleBidder)
			if !errors.Is(err, ErrAuthStorage) {
				t.Fatalf("Login() error = %v, want %v", err, ErrAuthStorage)
			}
			if strings.Contains(err.Error(), sensitivePath) {
				t.Fatalf("Login() error = %q, must not contain path %q", err.Error(), sensitivePath)
			}
		})
	}
}

func TestAuthRequireHidesTokenLookupErrors(t *testing.T) {
	const storageDetail = `open C:\secret\sessions.json: access denied`
	auth := NewAuthService(&authRepoStub{
		getUserErr: errors.New(storageDetail),
	})

	_, err := auth.Require("token", auction.RoleBidder)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Require() error = %v, want %v", err, ErrUnauthorized)
	}
	if strings.Contains(err.Error(), storageDetail) {
		t.Fatalf("Require() error = %q, must not contain %q", err.Error(), storageDetail)
	}
}

type authRepoStub struct {
	saveUserErr    error
	saveSessionErr error
	getUser        auction.User
	getUserErr     error
}

func (r *authRepoStub) SaveUser(auction.User) error {
	return r.saveUserErr
}

func (r *authRepoStub) SaveSession(auction.Session) error {
	return r.saveSessionErr
}

func (r *authRepoStub) GetUserByToken(string) (auction.User, error) {
	return r.getUser, r.getUserErr
}

func (r *authRepoStub) CreateAuction(a auction.Auction) (auction.Auction, error) {
	return a, nil
}

func (r *authRepoStub) UpdateAuction(auction.Auction) error {
	return nil
}

func (r *authRepoStub) GetAuction(string) (auction.Auction, error) {
	return auction.Auction{}, nil
}

func (r *authRepoStub) ListAuctions() ([]auction.Auction, error) {
	return nil, nil
}

func (r *authRepoStub) SaveBid(auction.Bid) error {
	return nil
}

func (r *authRepoStub) ListBids(string) ([]auction.Bid, error) {
	return nil, nil
}

func (r *authRepoStub) UpsertOrder(o auction.Order) (auction.Order, error) {
	return o, nil
}

func (r *authRepoStub) GetOrderByAuction(string) (auction.Order, error) {
	return auction.Order{}, nil
}
