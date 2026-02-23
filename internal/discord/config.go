package discord

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken      string
	GuildID           string
	LogChannelID      string
	SubsonicURL       string
	SubsonicUser      string
	SubsonicPassword  string
	FFmpegBitrate     int
	DefaultVolume     int
	CompressionLevel  int
	WordleAnswersPath string
	WordleAllowedPath string
	Database          string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN is required")
	}

	subURL := os.Getenv("SUBSONIC_URL")
	if subURL != "" {
		// Basic URL validation
		u, err := url.ParseRequestURI(subURL)
		if err != nil {
			return nil, fmt.Errorf("invalid SUBSONIC_URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("invalid SUBSONIC_URL scheme: %s (must be http or https)", u.Scheme)
		}
	}

	subUser := os.Getenv("SUBSONIC_USER")
	subPass := os.Getenv("SUBSONIC_PASSWORD")

	bitrate := 96 // Default 96kbps
	if brStr := os.Getenv("FFMPEG_BITRATE"); brStr != "" {
		if idx, err := strconv.Atoi(brStr); err == nil && idx > 0 {
			bitrate = idx
		}
	}

	vol := 256 // Default 100% (256/256)
	if volStr := os.Getenv("DEFAULT_VOLUME"); volStr != "" {
		if idx, err := strconv.Atoi(volStr); err == nil && idx >= 0 && idx <= 512 {
			vol = idx
		}
	}

	compressionLevel := 3 // Default for better performance
	if compStr := os.Getenv("COMPRESSION_LEVEL"); compStr != "" {
		if idx, err := strconv.Atoi(compStr); err == nil && idx >= 0 && idx <= 10 {
			compressionLevel = idx
		}
	}

	return &Config{
		DiscordToken:      token,
		GuildID:           os.Getenv("GUILD_ID"),
		LogChannelID:      os.Getenv("LOG_CHANNEL_ID"),
		SubsonicURL:       subURL,
		SubsonicUser:      subUser,
		SubsonicPassword:  subPass,
		FFmpegBitrate:     bitrate,
		DefaultVolume:     vol,
		CompressionLevel:  compressionLevel,
		WordleAnswersPath: getEnv("WORDLE_ANSWERS_PATH", "wordlist_answers.txt"),
		WordleAllowedPath: getEnv("WORDLE_ALLOWED_PATH", "wordlist_allowed.txt"),
		Database:          getEnv("DATABASE", "permissions.db"),
	}, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
