package ats

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"jobwatch/internal/model"
)

// Greenhouse career pages look like:
//
//	https://boards.greenhouse.io/{token}
//	https://job-boards.greenhouse.io/{token}
//	https://{token}.greenhouse.io/  (older embed style, rarer)
var greenhouseRe = regexp.MustCompile(`(?i)(?:boards|job-boards)\.greenhouse\.io/([a-z0-9\-_]+)`)

// DetectGreenhouse returns the board token and true if the URL is a
// Greenhouse-hosted career page.
func DetectGreenhouse(pageURL string) (token string, ok bool) {
	m := greenhouseRe.FindStringSubmatch(pageURL)
	if m == nil {
		return "", false
	}
	return m[1], true
}

type ghResponse struct {
	Jobs []struct {
		Title       string `json:"title"`
		AbsoluteURL string `json:"absolute_url"`
		Location    struct {
			Name string `json:"name"`
		} `json:"location"`
	} `json:"jobs"`
}

// FetchGreenhouseJobs calls Greenhouse's public, unauthenticated job board
// API directly. This is far more reliable than scraping the HTML page and
// costs zero Gemini quota.
func FetchGreenhouseJobs(token string) ([]model.Job, error) {
	url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=false", token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("calling greenhouse api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("greenhouse api returned status %d for board %q", resp.StatusCode, token)
	}

	var parsed ghResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding greenhouse response: %w", err)
	}

	jobs := make([]model.Job, 0, len(parsed.Jobs))
	for _, j := range parsed.Jobs {
		jobs = append(jobs, model.Job{
			Title:    j.Title,
			Location: j.Location.Name,
			URL:      j.AbsoluteURL,
		})
	}
	return jobs, nil
}
