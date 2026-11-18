package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func downloadClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Options{URL: url, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Evidence answers a redirect to storage elsewhere: followed, without the
// token.
func TestDownloadFollowsARedirectWithoutTheToken(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("the token went to the storage host")
		}
		if r.URL.Query().Get("sig") != "abc" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("GET /orders/2 HTTP/1.1"))
	}))
	defer storage.Close()
	hkr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken.Reveal() {
			t.Error("no token to HKr")
		}
		if r.URL.Path != "/api/v1/orgs/acme/findings/F/evidence/F-01/request.txt" || r.URL.Query().Get("scan") != "1" {
			t.Errorf("request = %s", r.URL)
		}
		http.Redirect(w, r, storage.URL+"/runs/x/request.txt?sig=abc", http.StatusFound)
	}))
	defer hkr.Close()

	got, err := downloadClient(t, hkr.URL).Download(t.Context(), "/orgs/acme/findings/F/evidence/F-01/request.txt", Query{"scan": "1"})
	if err != nil || string(got) != "GET /orders/2 HTTP/1.1" {
		t.Fatalf("Download = %q, %v", got, err)
	}
}

// An HKr that keeps evidence on its own disk answers the file itself.
func TestDownloadTakesTheFileItself(t *testing.T) {
	hkr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("png"))
	}))
	defer hkr.Close()
	got, err := downloadClient(t, hkr.URL).Download(t.Context(), "/x", nil)
	if err != nil || string(got) != "png" {
		t.Fatalf("Download = %q, %v", got, err)
	}
}

func TestDownloadRefusesOddRedirects(t *testing.T) {
	for _, loc := range []string{"file:///etc/passwd", ""} {
		hkr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
		}))
		_, err := downloadClient(t, hkr.URL).Download(t.Context(), "/x", nil)
		var e *Error
		if !errors.As(err, &e) || e.Code != "http_error" {
			t.Errorf("Location %q: err = %v", loc, err)
		}
		hkr.Close()
	}
}

// Storage refusing (an expired link) is an error that names it.
func TestDownloadReportsStorageErrors(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer storage.Close()
	hkr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+"/f", http.StatusFound)
	}))
	defer hkr.Close()
	_, err := downloadClient(t, hkr.URL).Download(t.Context(), "/x", nil)
	var e *Error
	if !errors.As(err, &e) || e.Status != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
}

// HKr's own errors come through as usual.
func TestDownloadReportsHKrErrors(t *testing.T) {
	hkr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Gone."}}`))
	}))
	defer hkr.Close()
	_, err := downloadClient(t, hkr.URL).Download(t.Context(), "/x", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
