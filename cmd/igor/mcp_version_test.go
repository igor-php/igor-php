package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const packagistBody = `{"packages":{"igor-php/igor-php":[
	{"version":"v0.11.0-beta1"},{"version":"dev-main"},{"version":"v0.10.1"},{"version":"v0.9.7"},{"version":"v0.10.0"}
]},"minified":"composer/2.0"}`

func fakePackagist(t *testing.T, body string, status int) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "igor-php/") {
			t.Errorf("unexpected User-Agent %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testChecker(t *testing.T, url string) *updateChecker {
	t.Setenv(noUpdateCheckEnv, "")
	c := newUpdateChecker()
	c.url = url
	c.cacheFile = filepath.Join(t.TempDir(), "latest-version.json")
	return c
}

func TestUpdateCheckerPicksLatestStableAndCaches(t *testing.T) {
	srv, calls := fakePackagist(t, packagistBody, http.StatusOK)
	c := testChecker(t, srv.URL)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	for range 2 {
		latest, err := c.latest(context.Background())
		if err != nil || latest != "0.10.1" {
			t.Fatalf("latest = %q, %v", latest, err)
		}
	}
	if *calls != 1 {
		t.Fatalf("the second call must use the cache, got %d requests", *calls)
	}

	now = now.Add(25 * time.Hour)
	if _, err := c.latest(context.Background()); err != nil || *calls != 2 {
		t.Fatalf("an expired cache must be refreshed: %v, %d requests", err, *calls)
	}
}

func TestUpdateCheckerFallsBackToStaleCache(t *testing.T) {
	srv, _ := fakePackagist(t, "oops", http.StatusInternalServerError)
	c := testChecker(t, srv.URL)
	if _, err := c.latest(context.Background()); err == nil {
		t.Fatal("expected an error without cache")
	}

	c.writeCache(latestVersionCache{Latest: "0.9.0", CheckedAt: time.Now().Add(-48 * time.Hour)})
	if latest, err := c.latest(context.Background()); err != nil || latest != "0.9.0" {
		t.Fatalf("expected the stale cache, got %q, %v", latest, err)
	}
}

func TestUpdateCheckerWaitsAfterAFailure(t *testing.T) {
	srv, calls := fakePackagist(t, "oops", http.StatusServiceUnavailable)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	newChecker := func(cacheFile string) *updateChecker {
		c := testChecker(t, srv.URL)
		c.cacheFile = cacheFile
		c.now = func() time.Time { return now }
		return c
	}

	for _, cacheFile := range []string{"", filepath.Join(t.TempDir(), "latest-version.json")} {
		*calls = 0
		c := newChecker(cacheFile)
		if _, err := c.latest(context.Background()); err == nil || *calls != 1 {
			t.Fatalf("first attempt: %v, %d requests", err, *calls)
		}
		now = now.Add(10 * time.Minute)
		if _, err := c.latest(context.Background()); err == nil || !strings.Contains(err.Error(), "next attempt") || *calls != 1 {
			t.Fatalf("a failure must not be retried before an hour: %v, %d requests", err, *calls)
		}
		if cacheFile != "" {
			// Another server process on the same machine waits too
			if _, err := newChecker(cacheFile).latest(context.Background()); err == nil || *calls != 1 {
				t.Fatalf("the failure must be shared through the cache: %v, %d requests", err, *calls)
			}
		}
		now = now.Add(time.Hour)
		if _, err := c.latest(context.Background()); err == nil || *calls != 2 {
			t.Fatalf("the check must be retried after an hour: %v, %d requests", err, *calls)
		}
	}
}

func TestUpdateCheckerCanBeDisabled(t *testing.T) {
	srv, calls := fakePackagist(t, packagistBody, http.StatusOK)
	c := testChecker(t, srv.URL)
	t.Setenv(noUpdateCheckEnv, "1")
	if _, err := c.latest(context.Background()); err == nil || *calls != 0 {
		t.Fatalf("the check must be disabled: %v, %d requests", err, *calls)
	}
}

func TestConstraintAllows(t *testing.T) {
	cases := []struct {
		constraint string
		version    string
		allows     bool
		known      bool
	}{
		{"^0.10", "0.10.1", true, true},
		{"^0.10", "0.11.0", false, true},
		{"^0.9.2", "0.10.0", false, true},
		{"^1.2", "1.9.0", true, true},
		{"^1.2", "2.0.0", false, true},
		{"^1.2", "1.1.0", false, true},
		{"~0.10.0", "0.10.4", true, true},
		{"~0.10.0", "0.11.0", false, true},
		{"~1.2", "1.5.0", true, true},
		{"0.10.*", "0.10.3", true, true},
		{"0.10.*", "0.11.0", false, true},
		{"0.10.1", "0.10.1", true, true},
		{"v0.10.1", "0.10.2", false, true},
		{">=0.9", "1.0.0", true, true},
		{"*", "3.0.0", true, true},
		{"^0.9 || ^0.10", "0.10.0", false, false},
		{"dev-main", "0.10.0", false, false},
	}
	for _, tc := range cases {
		v, _ := parseStableVersion(tc.version)
		allows, known := constraintAllows(tc.constraint, v)
		if allows != tc.allows || known != tc.known {
			t.Errorf("constraintAllows(%q, %s) = %v, %v; want %v, %v", tc.constraint, tc.version, allows, known, tc.allows, tc.known)
		}
	}
}

func TestCheckVersionUpdateCommand(t *testing.T) {
	srv, _ := fakePackagist(t, packagistBody, http.StatusOK)
	oldVersion := Version
	t.Cleanup(func() { Version = oldVersion })

	project := func(composer string) string {
		root := t.TempDir()
		if composer != "" {
			if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(composer), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}

	cases := []struct {
		name, version, composer string
		upToDate                *bool
		command, note           string
	}{
		{"latest", "0.10.1", `{"require-dev":{"igor-php/igor-php":"^0.10"}}`, boolPtr(true), "", ""},
		{"inside the constraint", "0.10.0", `{"require-dev":{"igor-php/igor-php":"^0.10"}}`, boolPtr(false), "composer update igor-php/igor-php", ""},
		{"outside the constraint", "0.9.7", `{"require-dev":{"igor-php/igor-php":"^0.9"}}`, boolPtr(false), "composer require --dev igor-php/igor-php:^0.10", "does not allow"},
		{"production requirement", "0.9.7", `{"require":{"igor-php/igor-php":"0.9.7"}}`, boolPtr(false), "composer require igor-php/igor-php:^0.10", "does not allow"},
		{"unknown constraint", "0.9.7", `{"require-dev":{"igor-php/igor-php":"^0.9 || ^0.10"}}`, boolPtr(false), "composer update igor-php/igor-php", "check that the constraint"},
		{"binary", "0.9.7", "", boolPtr(false), "go install github.com/igor-php/igor-php/cmd/igor@v0.10.1", "releases/latest"},
		{"dev build", "dev", "", nil, "", "development build"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Version = tc.version
			info := checkVersion(context.Background(), testChecker(t, srv.URL), project(tc.composer))
			if info.Latest != "0.10.1" || info.UpdateCommand != tc.command || !strings.Contains(info.Note, tc.note) {
				t.Fatalf("unexpected info: %+v", info)
			}
			if (info.UpToDate == nil) != (tc.upToDate == nil) || (info.UpToDate != nil && *info.UpToDate != *tc.upToDate) {
				t.Fatalf("up_to_date = %v, want %v", info.UpToDate, tc.upToDate)
			}
		})
	}
}
