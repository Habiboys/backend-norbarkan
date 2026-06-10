package service

import (
	"errors"
	"strings"

	passwordhash "backend-nobarkan/internal/pkg/bcrypt"
	"backend-nobarkan/internal/repository"
)

var (
	ErrOldPasswordWrong = errors.New("old password wrong")
)

type UserService struct {
	users *repository.UserRepository
}

func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) UpdateProfile(userID string, name string, avatarURL *string) (*UserResponse, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	name = strings.TrimSpace(name)
	if name != "" {
		user.Name = name
	}
	if avatarURL != nil {
		trimmed := strings.TrimSpace(*avatarURL)
		if trimmed == "" {
			user.AvatarURL = nil
		} else {
			user.AvatarURL = &trimmed
		}
	}

	if err := s.users.Update(user); err != nil {
		return nil, err
	}

	response := toUserResponse(user)
	return &response, nil
}

func (s *UserService) ChangePassword(userID string, oldPassword string, newPassword string) error {
	if len(newPassword) < 6 {
		return ErrInvalidCredentials
	}

	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}
	if !passwordhash.Compare(user.Password, oldPassword) {
		return ErrOldPasswordWrong
	}

	hashed, err := passwordhash.Hash(newPassword)
	if err != nil {
		return err
	}
	user.Password = hashed

	return s.users.Update(user)
}
