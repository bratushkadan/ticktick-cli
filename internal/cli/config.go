package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultBaseURL = "https://api.ticktick.com"

var dotenvValue = regexp.MustCompile(`^(?:"([^"]*)"|'([^']*)'|([^\s'"]+))(?:\s+#.*)?$`)

// readDotenv reads the supported TickTick settings without evaluating shell code.
func readDotenv(path string, content []byte) (map[string]string, error) {
	values := make(map[string]string)
	for i, line := range strings.Split(string(content), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		key = dotenvKey(key)
		switch key {
		case "TICKTICK_TOKEN", "TICKTICK_MCP_TOKEN", "TICKTICK_BASE_URL":
		default:
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "#") {
			values[key] = ""
			continue
		}
		match := dotenvValue.FindStringSubmatch(value)
		if match == nil {
			return nil, fmt.Errorf("%s:%d: invalid value for %s", path, i+1, key)
		}
		values[key] = strings.TrimSpace(firstValue(match[1], match[2], match[3]))
	}
	return values, nil
}

func dotenvKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "export ") || strings.HasPrefix(key, "export\t") {
		key = strings.TrimSpace(strings.TrimPrefix(key, "export"))
	}
	return key
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func configDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ticktick")
}

func loadDotenv(dir string) (map[string]string, error) {
	path := strings.TrimSpace(flagEnvFile)
	explicit := path != ""
	if !explicit {
		if dir == "" {
			return nil, nil
		}
		path = filepath.Join(dir, ".env")
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read TickTick env file: %w", err)
	}
	return readDotenv(path, content)
}

func readTokenFile(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	path := filepath.Join(dir, "token")
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read TickTick token file: %w", err)
	}
	token := strings.TrimSpace(string(content))
	key, _, _ := strings.Cut(token, "=")
	if key != "TICKTICK_TOKEN" && key != "TICKTICK_MCP_TOKEN" && !strings.ContainsAny(token, " \t\r\n") {
		return token, nil
	}
	values, err := readDotenv(path, content)
	if err != nil {
		return "", err
	}
	if token := firstValue(values["TICKTICK_TOKEN"], values["TICKTICK_MCP_TOKEN"]); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("%s: expected a raw token or a TICKTICK_TOKEN or TICKTICK_MCP_TOKEN assignment", path)
}

func resolveToken(dir string, values map[string]string) (string, error) {
	token := firstValue(flagToken, os.Getenv("TICKTICK_TOKEN"), os.Getenv("TICKTICK_MCP_TOKEN"),
		values["TICKTICK_TOKEN"], values["TICKTICK_MCP_TOKEN"])
	if token == "" {
		var err error
		token, err = readTokenFile(dir)
		if err != nil {
			return "", err
		}
	}
	if token == "" {
		return "", errors.New("no API token: set TICKTICK_TOKEN or TICKTICK_MCP_TOKEN, use --env-file, or configure ~/.config/ticktick/token")
	}
	return token, nil
}
