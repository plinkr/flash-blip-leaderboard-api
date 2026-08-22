package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func doGetVersion(t *testing.T, app *fiber.App) (*http.Response, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/game_version", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error testing endpoint: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return resp, string(body)
}

func newTestApp(t *testing.T, upstream http.HandlerFunc) *fiber.App {
	t.Helper()
	srv := httptest.NewServer(upstream)
	t.Cleanup(srv.Close)

	app := fiber.New()
	h := &GameVersionHandler{URL: srv.URL}
	app.Get("/game_version", h.Get)
	return app
}

func TestGameVersionFetchAndCache(t *testing.T) {
	var calls int
	app := newTestApp(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, `{"tag_name": "v0.8.7", "html_url": "https://github.com/plinkr/flash-blip/releases/tag/v0.8.7"}`)
		if err != nil {
			return
		}
	})

	resp, body := doGetVersion(t, app)
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if body != `{"tag_name":"v0.8.7"}` {
		t.Errorf("expected tag payload, got %q", body)
	}
	if calls != 1 {
		t.Errorf("expected 1 upstream call, got %d", calls)
	}

	resp2, body2 := doGetVersion(t, app)
	if resp2.StatusCode != 200 || body2 != `{"tag_name":"v0.8.7"}` {
		t.Errorf("cache hit mismatch: status=%d body=%q", resp2.StatusCode, body2)
	}
	if calls != 1 {
		t.Errorf("expected cached response without upstream call, got %d calls", calls)
	}
}

func TestGameVersionSendsUserAgent(t *testing.T) {
	var lastUA string
	var mu sync.Mutex
	app := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lastUA = r.Header.Get("User-Agent")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, `{"tag_name": "v1.2.3"}`)
		if err != nil {
			return
		}
	})

	doGetVersion(t, app)

	mu.Lock()
	defer mu.Unlock()
	if lastUA != githubAPIUserAgent {
		t.Errorf("expected User-Agent %q, got %q", githubAPIUserAgent, lastUA)
	}
}

func TestGameVersionStaleOnUpstreamFailure(t *testing.T) {
	var mu sync.Mutex
	var calls int
	fail := false
	upstream := func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		failing := fail
		mu.Unlock()

		if failing {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := fmt.Fprint(w, `{"message": "boom"}`)
			if err != nil {
				return
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, `{"tag_name": "v0.8.7"}`)
		if err != nil {
			return
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(upstream))
	t.Cleanup(srv.Close)

	app := fiber.New()
	h := &GameVersionHandler{URL: srv.URL, TTL: 50 * time.Millisecond}
	app.Get("/game_version", h.Get)

	resp, body := doGetVersion(t, app)
	if resp.StatusCode != 200 || body != `{"tag_name":"v0.8.7"}` {
		t.Fatalf("initial fetch failed: status=%d body=%q", resp.StatusCode, body)
	}

	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	fail = true
	mu.Unlock()

	staleResp, staleBody := doGetVersion(t, app)
	if staleResp.StatusCode != 200 || staleBody != `{"tag_name":"v0.8.7"}` {
		t.Errorf("expected stale value, got status=%d body=%q", staleResp.StatusCode, staleBody)
	}
	mu.Lock()
	if calls != 2 {
		t.Errorf("expected refresh attempt after TTL expiry, got %d calls", calls)
	}
	mu.Unlock()

	recentResp, recentBody := doGetVersion(t, app)
	if recentResp.StatusCode != 200 || recentBody != `{"tag_name":"v0.8.7"}` {
		t.Errorf("expected throttled stale value, got status=%d body=%q", recentResp.StatusCode, recentBody)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Errorf("expected upstream throttling after failure, got %d calls", calls)
	}
}

func TestGameVersionNoCacheUpstreamError(t *testing.T) {
	app := newTestApp(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, err := fmt.Fprint(w, `{"message": "API rate limit exceeded"}`)
		if err != nil {
			return
		}
	})

	resp, _ := doGetVersion(t, app)
	if resp.StatusCode != 502 {
		t.Errorf("expected status 502 without cached fallback, got %d", resp.StatusCode)
	}
}

func TestGameVersionMalformedBody(t *testing.T) {
	for name, body := range map[string]string{
		"invalid json": `<html>not json</html>`,
		"empty tag":    `{"html_url": "https://github.com/plinkr/flash-blip/releases/latest"}`,
	} {
		t.Run(name, func(t *testing.T) {
			app := newTestApp(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, err := fmt.Fprint(w, body)
				if err != nil {
					return
				}
			})

			resp, _ := doGetVersion(t, app)
			if resp.StatusCode != 502 {
				t.Errorf("expected status 502 for %s, got %d", name, resp.StatusCode)
			}
		})
	}
}
