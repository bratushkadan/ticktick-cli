package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const V2Origin = "https://ticktick.com"
const V2HistoryTimeLayout = "2006-01-02 15:04:05"
const V2DayLayout = "20060102"

// V2Client accesses the TickTick web API with its session cookie.
type V2Client struct {
	origin string
	cookie string
	http   http.Client
}

// NewV2Client accepts a Cookie header value or the value of the t cookie.
// A client with a nil transport uses the standard HTTP transport.
func NewV2Client(origin, cookie string, client *http.Client) (*V2Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("v2 API origin must be an absolute HTTPS origin")
	}
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return nil, errors.New("v2 API cookie is empty")
	}
	if strings.ContainsAny(cookie, "\r\n") {
		return nil, errors.New("v2 API cookie contains a newline")
	}
	if !strings.Contains(cookie, "=") {
		cookie = "t=" + cookie
	}
	c := V2Client{origin: strings.TrimRight(origin, "/"), cookie: cookie}
	if client != nil {
		c.http = *client
	}
	c.http.Jar = nil
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c, nil
}

type WebHTTPError struct {
	Status  int
	version string
}

type V2HTTPError = WebHTTPError

func (e *WebHTTPError) Error() string {
	version := e.version
	if version == "" {
		version = "v2"
	}
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("%s API error: HTTP %d; check TICKTICK_V2_API_COOKIE", version, e.Status)
	default:
		return fmt.Sprintf("%s API error: HTTP %d", version, e.Status)
	}
}

func v2Request[T any](ctx context.Context, c *V2Client, method, path string, query url.Values, body any) (T, error) {
	return webRequest[T](ctx, c, "v2", method, path, query, body)
}

func webRequest[T any](ctx context.Context, c *V2Client, version, method, path string, query url.Values, body any) (T, error) {
	var result T
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return result, err
		}
		reader = bytes.NewReader(data)
	}
	endpoint := c.origin + "/api/" + version + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return result, fmt.Errorf("could not construct %s API request", version)
	}
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// The URL in a transport error can include search terms and account IDs.
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			return result, fmt.Errorf("%s API request timed out", version)
		}
		return result, fmt.Errorf("%s API request failed", version)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, &WebHTTPError{Status: resp.StatusCode, version: version}
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("invalid %s API response", version)
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return result, fmt.Errorf("invalid %s API response", version)
	}
	return result, nil
}

// V2Task is the task metadata used by the v2 search and completion endpoints.
type V2Task struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"projectId,omitempty"`
	Status        int      `json:"status"`
	Priority      int      `json:"priority"`
	Kind          string   `json:"kind,omitempty"`
	DueDate       string   `json:"dueDate,omitempty"`
	CompletedTime string   `json:"completedTime,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	original      json.RawMessage
}

// OriginalJSON returns a copy of the original task object from a completion
// history or batch sync response.
func (t V2Task) OriginalJSON() json.RawMessage {
	return bytes.Clone(t.original)
}

func decodeWebTask(data []byte) (V2Task, int, error) {
	var record struct {
		V2Task
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return V2Task{}, 0, err
	}
	record.original = bytes.Clone(data)
	return record.V2Task, record.Deleted, nil
}

type V2SearchComment struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId,omitempty"`
}

type V2SearchHit struct {
	ID string `json:"id"`
}

type V2SearchResult struct {
	Tasks    []V2Task          `json:"tasks"`
	Comments []V2SearchComment `json:"comments"`
	Hits     []V2SearchHit     `json:"hits"`
}

type V2SearchOptions struct {
	Keywords         string
	DueFrom, DueTo   *int64
	Status           []int
	ProjectIDs, Tags []string
	IncludeClosed    bool
}

func (c *V2Client) Search(ctx context.Context, o V2SearchOptions) (V2SearchResult, error) {
	if strings.TrimSpace(o.Keywords) == "" {
		return V2SearchResult{}, nil
	}
	q := url.Values{"keywords": {o.Keywords}}
	if o.DueFrom != nil {
		q.Set("dueFrom", strconv.FormatInt(*o.DueFrom, 10))
	}
	if o.DueTo != nil {
		q.Set("dueTo", strconv.FormatInt(*o.DueTo, 10))
	}
	for _, status := range o.Status {
		q.Add("status", strconv.Itoa(status))
	}
	for _, id := range o.ProjectIDs {
		q.Add("projectId", id)
	}
	for _, tag := range o.Tags {
		q.Add("tags", tag)
	}
	if o.IncludeClosed {
		q.Set("includeClosed", "true")
	}
	return v2Request[V2SearchResult](ctx, c, http.MethodGet, "/search/all", q, nil)
}

type V2CompletedOptions struct {
	From  *time.Time
	To    time.Time
	Limit int
}

func (c *V2Client) CompletedInAll(ctx context.Context, o V2CompletedOptions) ([]V2Task, error) {
	return c.completed(ctx, "/project/all/completedInAll/", o)
}

// Completed uses the all/completed route when projectID is "all".
func (c *V2Client) Completed(ctx context.Context, projectID string, o V2CompletedOptions) ([]V2Task, error) {
	if projectID == "" || projectID == "." || projectID == ".." {
		return nil, errors.New("v2 completed history requires a project ID")
	}
	return c.completed(ctx, "/project/"+url.PathEscape(projectID)+"/completed/", o)
}

func (c *V2Client) completed(ctx context.Context, path string, o V2CompletedOptions) ([]V2Task, error) {
	if o.To.IsZero() || o.Limit < 1 {
		return nil, errors.New("v2 completed history requires an end time and a positive limit")
	}
	from := ""
	if o.From != nil {
		from = o.From.UTC().Format(V2HistoryTimeLayout)
	}
	q := url.Values{
		"from":  {from},
		"to":    {o.To.UTC().Format(V2HistoryTimeLayout)},
		"limit": {strconv.Itoa(o.Limit)},
	}
	raw, err := v2Request[[]json.RawMessage](ctx, c, http.MethodGet, path, q, nil)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	tasks := make([]V2Task, 0, len(raw))
	for _, data := range raw {
		task, _, err := decodeWebTask(data)
		if err != nil {
			return nil, errors.New("invalid v2 completed task")
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

type V2FocusDay struct {
	Day      string `json:"day"`
	Duration int64  `json:"duration"`
}

type V2FocusDistribution struct {
	ProjectDurations map[string]int64 `json:"projectDurations"`
	TaskDurations    map[string]int64 `json:"taskDurations"`
	TagDurations     map[string]int64 `json:"tagDurations"`
}

type V2FocusClock struct {
	TimeDurations map[int]int64 `json:"timeDurations"`
}

type V2FocusClockDay struct {
	Day string `json:"day"`
	V2FocusClock
}

type V2FocusGeneral struct {
	TodayPomoCount    int64 `json:"todayPomoCount"`
	TodayPomoDuration int64 `json:"todayPomoDuration"`
	TotalPomoCount    int64 `json:"totalPomoCount"`
	TotalPomoDuration int64 `json:"totalPomoDuration"`
}

func v2FocusRange[T any](ctx context.Context, c *V2Client, kind string, from, to time.Time) (T, error) {
	fromDay, toDay := from.Format(V2DayLayout), to.Format(V2DayLayout)
	if from.IsZero() || to.IsZero() || fromDay > toDay {
		var zero T
		return zero, errors.New("v2 focus statistics require an ordered date range")
	}
	return v2Request[T](ctx, c, http.MethodGet, "/pomodoros/statistics/"+kind+"/"+fromDay+"/"+toDay, nil, nil)
}

func (c *V2Client) FocusHeatmap(ctx context.Context, from, to time.Time) ([]V2FocusDay, error) {
	return v2FocusRange[[]V2FocusDay](ctx, c, "heatmap", from, to)
}

func (c *V2Client) FocusDistribution(ctx context.Context, from, to time.Time) (V2FocusDistribution, error) {
	return v2FocusRange[V2FocusDistribution](ctx, c, "dist", from, to)
}

func (c *V2Client) FocusClock(ctx context.Context, from, to time.Time) (V2FocusClock, error) {
	return v2FocusRange[V2FocusClock](ctx, c, "dist/clock", from, to)
}

func (c *V2Client) FocusClockByDay(ctx context.Context, from, to time.Time) ([]V2FocusClockDay, error) {
	return v2FocusRange[[]V2FocusClockDay](ctx, c, "dist/clockByDay", from, to)
}

func (c *V2Client) FocusGeneral(ctx context.Context) (V2FocusGeneral, error) {
	return v2Request[V2FocusGeneral](ctx, c, http.MethodGet, "/pomodoros/statistics/generalForDesktop", nil, nil)
}

type V2HabitCheckinOptions struct {
	HabitIDs   []string `json:"habitIds"`
	AfterStamp *int32   `json:"afterStamp,omitempty"`
}

type V2HabitCheckin struct {
	ID     string  `json:"id"`
	Stamp  int32   `json:"checkinStamp"`
	Value  float64 `json:"value"`
	Goal   float64 `json:"goal"`
	Status *int    `json:"status,omitempty"`
}

type V2HabitCheckins struct {
	Checkins map[string][]V2HabitCheckin `json:"checkins"`
}

func (c *V2Client) HabitCheckins(ctx context.Context, o V2HabitCheckinOptions) (V2HabitCheckins, error) {
	if o.HabitIDs == nil {
		o.HabitIDs = []string{}
	}
	return v2Request[V2HabitCheckins](ctx, c, http.MethodPost, "/habitCheckins/query", nil, o)
}
