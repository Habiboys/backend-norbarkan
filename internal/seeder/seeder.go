package seeder

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Seeder struct {
	db *gorm.DB
}

type UserSeed struct {
	ID       string
	Name     string
	Email    string
	Password string
}

func New(db *gorm.DB) *Seeder {
	return &Seeder{db: db}
}

func (s *Seeder) Run() error {
	if err := s.seedUsers(); err != nil {
		return err
	}

	return nil
}

func (s *Seeder) seedUsers() error {
	users := []UserSeed{
		{
			ID:       "11111111-1111-1111-1111-111111111111",
			Name:     "Admin NobarSync",
			Email:    "admin@nobarsync.local",
			Password: "password123",
		},
		{
			ID:       "22222222-2222-2222-2222-222222222222",
			Name:     "Nouval Demo",
			Email:    "nouval@example.com",
			Password: "password123",
		},
	}

	for _, user := range users {
		if err := s.upsertUser(user); err != nil {
			return err
		}
	}

	return nil
}

func (s *Seeder) upsertUser(seed UserSeed) error {
	if _, err := uuid.Parse(seed.ID); err != nil {
		return fmt.Errorf("invalid seed user id %s: %w", seed.ID, err)
	}

	var existingID string
	err := s.db.Table("users").Select("id").Where("email = ?", seed.Email).Take(&existingID).Error
	if err == nil {
		fmt.Printf("skip user %s, sudah ada\n", seed.Email)
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("cek user %s: %w", seed.Email, err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(seed.Password), 12)
	if err != nil {
		return fmt.Errorf("hash password user %s: %w", seed.Email, err)
	}

	result := s.db.Exec(`
INSERT INTO users (id, name, email, password, is_active)
VALUES (?, ?, ?, ?, TRUE)
`, seed.ID, seed.Name, seed.Email, string(hashedPassword))
	if result.Error != nil {
		return fmt.Errorf("insert user %s: %w", seed.Email, result.Error)
	}

	fmt.Printf("seeded user %s\n", seed.Email)
	return nil
}
