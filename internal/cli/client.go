package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"ticktick-cli/api"
)

var apiTransport http.RoundTripper

// newClient builds the generated API client with bearer auth over HTTPS.
func newClient() (*api.ClientWithResponses, error) {
	baseURL := resolveBaseURL()
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Hostname() == "" {
		return nil, fmt.Errorf("API base URL must be an absolute HTTPS URL")
	}

	token, err := resolveToken()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{
		Timeout:   flagTimeout,
		Transport: apiTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("API redirect must use HTTPS")
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}
	return api.NewClientWithResponses(
		baseURL,
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
