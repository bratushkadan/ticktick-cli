package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const defaultBaseURL = "https://api.ticktick.com"

// resolveToken: --token flag > TICKTICK_TOKEN env > ~/.config/ticktick/token.
func resolveToken() (string, error) {
	if strings.TrimSpace(flagToken) != "" {
		return strings.TrimSpace(flagToken), nil
	}
	if v := strings.TrimSpace(os.Getenv("TICKTICK_TOKEN")); v != "" {
		return v, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".config", "ticktick", "token")
		if b, err := os.ReadFile(p); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				return t, nil
			}
		}
	}
	return "", errors.New("no API token: pass --token, set TICKTICK_TOKEN, or write it to ~/.config/ticktick/token (TickTick web app > Settings > Account > API Token)")
}

// resolveBaseURL: --base-url flag > TICKTICK_BASE_URL env > default.
func resolveBaseURL() string {
	if strings.TrimSpace(flagBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(flagBaseURL), "/")
	}
	if v := strings.TrimSpace(os.Getenv("TICKTICK_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBaseURL
}