package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

func TestAuthSeedsDemoUsersAndLogsInWithJWT(t *testing.T) {
	repo := repository.NewMemoryRepository()
	auth := NewAuthService(repo, "demo-secret", 24*time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatalf("SeedDemoUsers() error = %v", err)
	}

	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Token == "" {
		t.Fatal("Login() returned empty token")
	}
	if session.User.ID != "usr-user-a" || session.User.DisplayName != "用户A" || session.User.Role != auction.RoleBidder {
		t.Fatalf("Login() user = %+v", session.User)
	}

	user, err := auth.Require(session.Token, auction.RoleBidder)
	if err != nil {
		t.Fatalf("Require() error = %v", err)
	}
	if user.ID != "usr-user-a" {
		t.Fatalf("Require() user id = %s, want usr-user-a", user.ID)
	}
}

func TestAuthLoginRejectsWrongPassword(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository(), "demo-secret", time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Login("userA", "bad-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want %v", err, ErrInvalidCredentials)
	}
}

func TestAuthRequireRejectsWrongRole(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository(), "demo-secret", time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Require(session.Token, auction.RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Require(admin) error = %v, want %v", err, ErrForbidden)
	}
}

func TestAuthRequireRejectsExpiredJWT(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository(), "demo-secret", -time.Second)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Require(session.Token, auction.RoleBidder); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Require(expired) error = %v, want %v", err, ErrTokenExpired)
	}
}

func TestAuthLoginRejectsDisabledUser(t *testing.T) {
	repo := repository.NewMemoryRepository()
	auth := NewAuthService(repo, "demo-secret", time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	user, err := repo.GetUserByUsername("userA")
	if err != nil {
		t.Fatal(err)
	}
	user.Status = auction.UserDisabled
	if err := repo.UpsertUser(user); err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Login("userA", "123456"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Login(disabled) error = %v, want %v", err, ErrUserDisabled)
	}
}

func TestAuthSeedHidesStorageErrors(t *testing.T) {
	const sensitivePath = `C:\secret\users.json`
	auth := NewAuthService(&authRepoStub{
		upsertUserErr: errors.New("open " + sensitivePath + ": access denied"),
	}, "demo-secret", time.Hour)

	err := auth.SeedDemoUsers()
	if !errors.Is(err, ErrAuthStorage) {
		t.Fatalf("SeedDemoUsers() error = %v, want %v", err, ErrAuthStorage)
	}
	if strings.Contains(err.Error(), sensitivePath) {
		t.Fatalf("SeedDemoUsers() error = %q, must not contain path %q", err.Error(), sensitivePath)
	}
}

func TestAuthRequireHidesUserLookupErrors(t *testing.T) {
	const storageDetail = `open C:\secret\users.json: access denied`
	repo := repository.NewMemoryRepository()
	auth := NewAuthService(repo, "demo-secret", time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatal(err)
	}

	broken := NewAuthService(&authRepoStub{
		getUserErr: errors.New(storageDetail),
	}, "demo-secret", time.Hour)
	_, err = broken.Require(session.Token, auction.RoleBidder)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Require() error = %v, want %v", err, ErrUnauthorized)
	}
	if strings.Contains(err.Error(), storageDetail) {
		t.Fatalf("Require() error = %q, must not contain %q", err.Error(), storageDetail)
	}
}

type authRepoStub struct {
	upsertUserErr error
	getUser       auction.User
	getUserErr    error
}

func (r *authRepoStub) SaveUser(user auction.User) error {
	return r.UpsertUser(user)
}

func (r *authRepoStub) UpsertUser(auction.User) error {
	return r.upsertUserErr
}

func (r *authRepoStub) SaveSession(auction.Session) error {
	return nil
}

func (r *authRepoStub) GetUserByToken(string) (auction.User, error) {
	return r.getUser, r.getUserErr
}

func (r *authRepoStub) GetUser(string) (auction.User, error) {
	return r.getUser, r.getUserErr
}

func (r *authRepoStub) GetUserByUsername(string) (auction.User, error) {
	return r.getUser, r.getUserErr
}

func (r *authRepoStub) ListUsers() ([]auction.User, error) {
	return nil, nil
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
