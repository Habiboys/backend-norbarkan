package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/domain"
	passwordhash "backend-nobarkan/internal/pkg/bcrypt"
	jwtutil "backend-nobarkan/internal/pkg/jwt"
	"backend-nobarkan/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrEmailAlreadyRegistered = errors.New("email already registered")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrUserNotFound           = errors.New("user not found")
	ErrRefreshTokenInvalid    = errors.New("refresh token invalid")
)

type AuthService struct {
	users  *repository.UserRepository
	tokens *repository.RefreshTokenRepository
	cfg    config.JWTConfig
}

type AuthResult struct {
	User         UserResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
}

type RefreshResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type UserResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarURL *string `json:"avatar_url,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
}

func NewAuthService(users *repository.UserRepository, tokens *repository.RefreshTokenRepository, cfg config.JWTConfig) *AuthService {
	return &AuthService{users: users, tokens: tokens, cfg: cfg}
}

func (s *AuthService) Register(name string, email string, password string) (*AuthResult, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	if name == "" || email == "" || len(password) < 6 {
		return nil, fmt.Errorf("invalid register payload")
	}

	existing, err := s.users.FindByEmail(email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrEmailAlreadyRegistered
	}

	hashedPassword, err := passwordhash.Hash(password)
	if err != nil {
		return nil, err
	}

	user := &domain.User{
		ID:       uuid.NewString(),
		Name:     name,
		Email:    email,
		Password: hashedPassword,
		IsActive: true,
	}

	if err := s.users.Create(user); err != nil {
		return nil, err
	}

	return s.issueAuthResult(user)
}

func (s *AuthService) Login(email string, password string) (*AuthResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.users.FindByEmail(email)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive || !passwordhash.Compare(user.Password, password) {
		return nil, ErrInvalidCredentials
	}

	return s.issueAuthResult(user)
}

func (s *AuthService) Refresh(refreshToken string) (*RefreshResult, error) {
	claims, err := jwtutil.Parse(refreshToken, s.cfg.RefreshSecret)
	if err != nil {
		return nil, ErrRefreshTokenInvalid
	}

	tokenHash := hashToken(refreshToken)
	stored, err := s.tokens.FindByTokenHash(tokenHash)
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.UserID != claims.UserID || time.Now().After(stored.ExpiresAt) {
		return nil, ErrRefreshTokenInvalid
	}

	user, err := s.users.FindByID(claims.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, ErrRefreshTokenInvalid
	}

	if err := s.tokens.DeleteByTokenHash(tokenHash); err != nil {
		return nil, err
	}

	accessToken, newRefreshToken, err := s.issueTokens(user)
	if err != nil {
		return nil, err
	}

	return &RefreshResult{AccessToken: accessToken, RefreshToken: newRefreshToken}, nil
}

func (s *AuthService) Logout(refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	return s.tokens.DeleteByTokenHash(hashToken(refreshToken))
}

func (s *AuthService) Me(userID string) (*UserResponse, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	response := toUserResponse(user)
	return &response, nil
}

func (s *AuthService) issueAuthResult(user *domain.User) (*AuthResult, error) {
	accessToken, refreshToken, err := s.issueTokens(user)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User:         toUserResponse(user),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *AuthService) issueTokens(user *domain.User) (string, string, error) {
	accessToken, err := jwtutil.Generate(user.ID, user.Email, s.cfg.AccessSecret, s.cfg.AccessExpired)
	if err != nil {
		return "", "", err
	}

	refreshToken, err := jwtutil.Generate(user.ID, user.Email, s.cfg.RefreshSecret, s.cfg.RefreshExpired)
	if err != nil {
		return "", "", err
	}

	stored := &domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		Token:     hashToken(refreshToken),
		ExpiresAt: time.Now().Add(s.cfg.RefreshExpired),
	}
	if err := s.tokens.Create(stored); err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

func toUserResponse(user *domain.User) UserResponse {
	return UserResponse{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		AvatarURL: user.AvatarURL,
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
