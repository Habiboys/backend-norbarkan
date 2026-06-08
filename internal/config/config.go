package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	App     AppConfig
	DB      DatabaseConfig
	Redis   RedisConfig
	JWT     JWTConfig
	Storage StorageConfig
	Upload  UploadConfig
}

type AppConfig struct {
	Port   string
	Env    string
	Secret string
}

type DatabaseConfig struct {
	Host string
	Port string
	User string
	Pass string
	Name string
}

type RedisConfig struct {
	Host string
	Port string
	Pass string
}

type JWTConfig struct {
	AccessSecret  string
	RefreshSecret string
	AccessExpired time.Duration
	RefreshExpired time.Duration
}

type StorageConfig struct {
	Type    string
	Path    string
	BaseURL string
}

type UploadConfig struct {
	MaxSizeGB      int
	MaxUploadPerHr int
}

func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	accessExpired, err := time.ParseDuration(v.GetString("JWT_ACCESS_EXPIRED"))
	if err != nil {
		return nil, fmt.Errorf("parse JWT_ACCESS_EXPIRED: %w", err)
	}

	refreshExpired, err := time.ParseDuration(v.GetString("JWT_REFRESH_EXPIRED"))
	if err != nil {
		return nil, fmt.Errorf("parse JWT_REFRESH_EXPIRED: %w", err)
	}

	return &Config{
		App: AppConfig{
			Port:   v.GetString("APP_PORT"),
			Env:    v.GetString("APP_ENV"),
			Secret: v.GetString("APP_SECRET"),
		},
		DB: DatabaseConfig{
			Host: v.GetString("DB_HOST"),
			Port: v.GetString("DB_PORT"),
			User: v.GetString("DB_USER"),
			Pass: v.GetString("DB_PASS"),
			Name: v.GetString("DB_NAME"),
		},
		Redis: RedisConfig{
			Host: v.GetString("REDIS_HOST"),
			Port: v.GetString("REDIS_PORT"),
			Pass: v.GetString("REDIS_PASS"),
		},
		JWT: JWTConfig{
			AccessSecret:  v.GetString("JWT_ACCESS_SECRET"),
			RefreshSecret: v.GetString("JWT_REFRESH_SECRET"),
			AccessExpired: accessExpired,
			RefreshExpired: refreshExpired,
		},
		Storage: StorageConfig{
			Type:    v.GetString("STORAGE_TYPE"),
			Path:    v.GetString("STORAGE_PATH"),
			BaseURL: v.GetString("STORAGE_BASE_URL"),
		},
		Upload: UploadConfig{
			MaxSizeGB:      v.GetInt("MAX_UPLOAD_SIZE_GB"),
			MaxUploadPerHr: v.GetInt("MAX_UPLOAD_PER_HOUR"),
		},
	}, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("APP_PORT", "8080")
	v.SetDefault("APP_ENV", "development")
	v.SetDefault("APP_SECRET", "change-me")
	v.SetDefault("DB_HOST", "localhost")
	v.SetDefault("DB_PORT", "3306")
	v.SetDefault("DB_USER", "root")
	v.SetDefault("DB_PASS", "password")
	v.SetDefault("DB_NAME", "nobarsync")
	v.SetDefault("REDIS_HOST", "localhost")
	v.SetDefault("REDIS_PORT", "6379")
	v.SetDefault("REDIS_PASS", "")
	v.SetDefault("JWT_ACCESS_SECRET", "access-secret")
	v.SetDefault("JWT_REFRESH_SECRET", "refresh-secret")
	v.SetDefault("JWT_ACCESS_EXPIRED", "15m")
	v.SetDefault("JWT_REFRESH_EXPIRED", "168h")
	v.SetDefault("STORAGE_TYPE", "local")
	v.SetDefault("STORAGE_PATH", "./storage")
	v.SetDefault("STORAGE_BASE_URL", "http://localhost:8080/stream")
	v.SetDefault("MAX_UPLOAD_SIZE_GB", 5)
	v.SetDefault("MAX_UPLOAD_PER_HOUR", 3)
}
