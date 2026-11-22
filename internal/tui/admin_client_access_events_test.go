package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPAdminClientListAccessEvents(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, time.November, 21, 10, 0, 0, 0, time.UTC)
	session := AdminSession{Username: "operator", BearerToken: "bearer-token", ExpiresAt: fixedNow.Add(10 * time.Minute)}
	newClient := func(t *testing.T, handler http.HandlerFunc) *HTTPAdminClient {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := NewHTTPAdminClient(server.URL, server.Client())
		if err != nil {
			t.Fatalf("NewHTTPAdminClient() error = %v", err)
		}
		client.now = func() time.Time { return fixedNow }
		return client
	}

	t.Run("sends the limit and decodes the rows", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/admin/v1/access-events" || r.URL.Query().Get("limit") != "20" {
				t.Fatalf("request = %s %s", r.Method, r.URL.String())
			}
			if got := r.Header.Get("Authorization"); got != "Bearer bearer-token" {
				t.Fatalf("Authorization = %q", got)
			}
			_, _ = w.Write([]byte(`[{"id":"e1","at":"2026-11-21T07:00:00Z","action":"grant.put","actor":"admin","subject":"alice","repository":"team/api","role":"writer"},{"id":"e2","at":"2026-11-20T07:00:00Z","action":"token.issued","actor":"admin","token_name":"ci","detail":"30 days"}]`))
		})
		events, err := client.ListAccessEvents(context.Background(), session, 20)
		if err != nil {
			t.Fatalf("ListAccessEvents() error = %v", err)
		}
		if len(events) != 2 || events[0].Action != "grant.put" || events[0].Subject != "alice" || events[0].Role != "writer" || events[1].TokenName != "ci" || events[1].Detail != "30 days" || events[0].At.Hour() != 7 {
			t.Fatalf("events = %#v", events)
		}
	})

	t.Run("omits a zero limit and clamps a large one", func(t *testing.T) {
		t.Parallel()
		var queries []string
		client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			queries = append(queries, r.URL.RawQuery)
			_, _ = w.Write([]byte(`[]`))
		})
		for _, limit := range []int{0, 5000} {
			if _, err := client.ListAccessEvents(context.Background(), session, limit); err != nil {
				t.Fatalf("ListAccessEvents(%d) error = %v", limit, err)
			}
		}
		if len(queries) != 2 || queries[0] != "" || queries[1] != "limit=200" {
			t.Fatalf("queries = %q, want none then limit=200", queries)
		}
	})

	t.Run("an API error is returned", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
		if _, err := client.ListAccessEvents(context.Background(), session, 20); err == nil {
			t.Fatal("ListAccessEvents() error = nil, want the API error")
		}
	})
}
