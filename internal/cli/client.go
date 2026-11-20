package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"ticktick-cli/api"
)

// newClient builds the generated API client with bearer auth and timeout.
func newClient() (*api.ClientWithResponses, error) {
	token, err := resolveToken()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: flagTimeout}
	return api.NewClientWithResponses(
		resolveBaseURL(),
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}),
	)
}

// APIError is a non-2xx API response.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	if body == "" {
		return fmt.Sprintf("API error: HTTP %d", e.Status)
	}
	return fmt.Sprintf("API error: HTTP %d: %s", e.Status, body)
}

// CheckHTTP converts a non-2xx response into an *APIError.
// Usage in commands:
//
//	resp, err := c.GetTaskWithResponse(ctx, projectId, taskId)
//	if err != nil { return err }
//	if err := CheckHTTP(resp.StatusCode(), resp.Body); err != nil { return err }
//	return Render(resp.Body, func(w io.Writer) { ... })
func CheckHTTP(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	return &APIError{Status: status, Body: string(body)}
}
