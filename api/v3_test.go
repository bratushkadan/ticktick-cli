package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBatchCheck(t *testing.T) {
	const checkpoint = int64(1795220000000)
	requests := make(chan string, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		if r.Method != http.MethodGet || r.Header.Get("Cookie") != "t=test-cookie" || r.ContentLength != 0 {
			t.Error("expected a GET with a session cookie and no body")
		}
		fmt.Fprintf(w, `{"checkPoint":%d,"syncTaskBean":{"update":[{"id":"t1","title":"Example","status":0,"deleted":0}]}}`, checkpoint)
	}))
	defer server.Close()
	client, err := NewV2Client(server.URL, "test-cookie", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	initial, err := client.BatchCheck(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if initial.CheckPoint != checkpoint || len(initial.SyncTaskBean.Update) != 1 ||
		string(initial.SyncTaskBean.Update[0].OriginalJSON()) != `{"id":"t1","title":"Example","status":0,"deleted":0}` {
		t.Fatal("expected a checkpoint and the full original task")
	}
	if _, err := client.BatchCheck(context.Background(), initial.CheckPoint); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/api/v3/batch/check/0", fmt.Sprintf("/api/v3/batch/check/%d", checkpoint)} {
		if got := <-requests; got != want {
			t.Errorf("request target = %s, want %s", got, want)
		}
	}
}
