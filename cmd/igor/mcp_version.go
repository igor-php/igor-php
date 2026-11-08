package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	packageName        = "igor-php/igor-php"
	packagistURL       = "https://repo.packagist.org/p2/igor-php/igor-php.json"
	updateCheckTTL     = 24 * time.Hour
	updateCheckTimeout = 2 * time.Second
	// updateRetryDelay spaces the attempts when Packagist cannot be reached, so an offline machine does not
	// wait for the timeout on every call.
	updateRetryDelay = time.Hour
	// noUpdateCheckEnv disables the call to Packagist (offline machines, CI).
	noUpdateCheckEnv = "IGOR_NO_UPDATE_CHECK"
)

// versionInfo tells the agent which igor runs, the latest release and how to update. igor never updates
// itself: the agent proposes the command to the developer.
type versionInfo struct {
	Version       string `json:"version" jsonschema:"version of the running igor binary"`
	Latest        string `json:"latest,omitempty" jsonschema:"latest stable release on Packagist"`
	UpToDate      *bool  `json:"up_to_date,omitempty" jsonschema:"whether the running igor is the latest release; absent when unknown"`
	Install       string `json:"install" jsonschema:"how igor is installed in this project: composer or binary"`
	Constraint    string `json:"constraint,omitempty" jsonschema:"version constraint of igor-php/igor-php in composer.json"`
	UpdateCommand string `json:"update_command,omitempty" jsonschema:"command to propose to the developer to update igor; never run it without their approval"`
	Note          string `json:"note,omitempty"`
}

// updateChecker reads the latest release of igor from Packagist, cached for a day.
type updateChecker struct {
	url       string
	cacheFile string
	client    *http.Client
	now       func() time.Time
	// failedAt is the last failed attempt, also kept here when there is no cache file.
	failedAt time.Time
}

func newUpdateChecker() *updateChecker {
	cacheFile := ""
	if dir, err := os.UserCacheDir(); err == nil {
		cacheFile = filepath.Join(dir, "igor-php", "latest-version.json")
	}
	return &updateChecker{
		url:       packagistURL,
		cacheFile: cacheFile,
		client:    &http.Client{Timeout: updateCheckTimeout},
		now:       time.Now,
	}
}

type latestVersionCache struct {
	Latest    string    `json:"latest,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	FailedAt  time.Time `json:"failed_at,omitzero"`
}

// latest returns the latest stable release of igor. A fresh cache answers without network; when
// Packagist cannot be reached, a stale cache is better than nothing, and the next attempt waits an hour.
func (c *updateChecker) latest(ctx context.Context) (string, error) {
	if v := os.Getenv(noUpdateCheckEnv); v != "" && v != "0" {
		return "", fmt.Errorf("update check disabled by %s", noUpdateCheckEnv)
	}

	now := c.now()
	cached := c.readCache()
	if cached.Latest != "" && now.Sub(cached.CheckedAt) < updateCheckTTL {
		return cached.Latest, nil
	}
	if lastFailure := latestTime(cached.FailedAt, c.failedAt); now.Sub(lastFailure) < updateRetryDelay {
		if cached.Latest != "" {
			return cached.Latest, nil
		}
		return "", fmt.Errorf("packagist could not be reached at %s, next attempt after %s", lastFailure.Format(time.RFC3339), lastFailure.Add(updateRetryDelay).Format(time.RFC3339))
	}

	latest, err := c.fetch(ctx)
	if err != nil {
		c.failedAt, cached.FailedAt = now, now
		c.writeCache(cached)
		if cached.Latest != "" {
			return cached.Latest, nil
		}
		return "", err
	}
	c.writeCache(latestVersionCache{Latest: latest, CheckedAt: c.now()})
	return latest, nil
}

func (c *updateChecker) fetch(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "igor-php/"+Version+" (update check)")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach Packagist: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("packagist answered %s", resp.Status)
	}

	var metadata struct {
		Packages map[string][]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return "", fmt.Errorf("unexpected Packagist answer: %w", err)
	}
	var best []int
	latest := ""
	for _, release := range metadata.Packages[packageName] {
		parts, ok := parseStableVersion(release.Version)
		if ok && (best == nil || compareVersions(parts, best) > 0) {
			best, latest = parts, strings.TrimPrefix(release.Version, "v")
		}
	}
	if latest == "" {
		return "", fmt.Errorf("no stable release of %s on Packagist", packageName)
	}
	return latest, nil
}

func (c *updateChecker) readCache() latestVersionCache {
	var cached latestVersionCache
	if c.cacheFile == "" {
		return cached
	}
	if data, err := os.ReadFile(c.cacheFile); err == nil && json.Unmarshal(data, &cached) != nil {
		return latestVersionCache{}
	}
	return cached
}

func latestTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (c *updateChecker) writeCache(cached latestVersionCache) {
	if c.cacheFile == "" {
		return
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(c.cacheFile), 0o755) == nil {
		_ = os.WriteFile(c.cacheFile, data, 0o644)
	}
}

var stableVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// parseStableVersion parses X.Y.Z (with an optional v); pre-releases and dev builds are not stable.
func parseStableVersion(v string) ([]int, bool) {
	m := stableVersionPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return nil, false
	}
	parts := make([]int, 3)
	for i := range parts {
		parts[i], _ = strconv.Atoi(m[i+1])
	}
	return parts, true
}

func compareVersions(a, b []int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// composerRequirement returns the constraint of igor-php/igor-php in the composer.json of the project,
// and whether it is a dev requirement.
func composerRequirement(rootPath string) (constraint string, dev bool, found bool) {
	data, err := os.ReadFile(filepath.Join(rootPath, "composer.json"))
	if err != nil {
		return "", false, false
	}
	var manifest struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return "", false, false
	}
	if c, ok := manifest.Require[packageName]; ok {
		return c, false, true
	}
	if c, ok := manifest.RequireDev[packageName]; ok {
		return c, true, true
	}
	return "", false, false
}

// constraintAllows tells whether a usual Composer constraint (^X.Y, ~X.Y, X.Y.*, X.Y.Z, >=X.Y) accepts
// version. known is false for a constraint it does not understand.
func constraintAllows(constraint string, version []int) (allows bool, known bool) {
	c := strings.TrimSpace(constraint)
	if c == "*" || c == "" {
		return true, true
	}
	if strings.ContainsAny(c, "|, <") {
		return false, false
	}

	operator := ""
	for _, op := range []string{">=", "^", "~", "v"} {
		if strings.HasPrefix(c, op) {
			operator, c = op, strings.TrimPrefix(c, op)
			break
		}
	}
	wildcard := strings.HasSuffix(c, ".*")
	base, fields, ok := parseConstraintVersion(strings.TrimSuffix(c, ".*"))
	if !ok {
		return false, false
	}

	switch {
	case operator == ">=":
		return compareVersions(version, base) >= 0, true
	case wildcard:
		// X.* or X.Y.*: the given fields must match
		return samePrefix(version, base, fields), true
	case operator == "^":
		// The first non-zero field is fixed (^1.2 allows 1.x, ^0.10 allows 0.10.x only)
		fixed := fields
		for i := range fields {
			if base[i] != 0 {
				fixed = i + 1
				break
			}
		}
		return compareVersions(version, base) >= 0 && samePrefix(version, base, fixed), true
	case operator == "~":
		// ~X.Y allows X.*, ~X.Y.Z allows X.Y.*
		return compareVersions(version, base) >= 0 && samePrefix(version, base, max(fields-1, 1)), true
	default:
		// Exact version
		return fields == 3 && compareVersions(version, base) == 0, true
	}
}

// parseConstraintVersion parses X, X.Y or X.Y.Z, missing fields being 0, and returns how many were given.
func parseConstraintVersion(v string) ([]int, int, bool) {
	fields := strings.Split(v, ".")
	if len(fields) > 3 {
		return nil, 0, false
	}
	base := make([]int, 3)
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, 0, false
		}
		base[i] = n
	}
	return base, len(fields), true
}

// samePrefix tells whether the first n fields of two versions are equal.
func samePrefix(a, b []int, n int) bool {
	for i := range n {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkVersion compares the running igor with the latest release and builds the command that updates it
// the way the project installs it.
func checkVersion(ctx context.Context, checker *updateChecker, rootPath string) versionInfo {
	info := versionInfo{Version: Version, Install: "binary"}
	constraint, dev, viaComposer := composerRequirement(rootPath)
	if viaComposer {
		info.Install, info.Constraint = "composer", constraint
	}

	latest, err := checker.latest(ctx)
	if err != nil {
		info.Note = err.Error()
		return info
	}
	info.Latest = latest
	latestParts, _ := parseStableVersion(latest)

	current, ok := parseStableVersion(Version)
	if !ok {
		info.Note = fmt.Sprintf("igor %s is a development build: it cannot be compared with the releases", Version)
		return info
	}
	upToDate := compareVersions(current, latestParts) >= 0
	info.UpToDate = &upToDate
	if upToDate {
		return info
	}

	if !viaComposer {
		info.UpdateCommand = "go install github.com/igor-php/igor-php/cmd/igor@v" + latest
		info.Note = "or download the binary from https://github.com/igor-php/igor-php/releases/latest"
		return info
	}
	if allows, known := constraintAllows(constraint, latestParts); allows || !known {
		info.UpdateCommand = "composer update " + packageName
		if !known {
			info.Note = fmt.Sprintf("check that the constraint %q allows %s, or widen it with composer require", constraint, latest)
		}
		return info
	}
	devFlag := ""
	if dev {
		devFlag = "--dev "
	}
	info.UpdateCommand = fmt.Sprintf("composer require %s%s:^%d.%d", devFlag, packageName, latestParts[0], latestParts[1])
	info.Note = fmt.Sprintf("the constraint %q does not allow %s: the command widens it; read the changelog first", constraint, latest)
	return info
}
