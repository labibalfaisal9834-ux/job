package ats

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"jobwatch/internal/model"
)

// Lever career pages look like:
//
//	https://jobs.lever.co/{company}
var leverRe = regexp.MustCompile(`(?i)jobs\.lever\.co/([a-z0-9\-_]+)`)

// DetectLever returns the company slug and true if the URL is a
// Lever-hosted career page.
func DetectLever(pageURL string) (company string, ok bool) {
	m := leverRe.FindStringSubmatch(pageURL)
	if m == nil {
		return "", false
	}
	return m[1], true
}

type leverPosting struct {
	Text       string `json:"text"`
	HostedURL  string `json:"hostedUrl"`
	Categories struct {
		Location string `json:"location"`
	} `json:"categories"`
}

// FetchLeverJobs calls Lever's public, unauthenticated postings API
// directly, avoiding both HTML scraping and Gemini quota usage.
func FetchLeverJobs(company string) ([]model.Job, error) {
	url := fmt.Sprintf("https://api.lever.co/v0/postings/%s?mode=json", company)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("calling lever api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lever api returned status %d for company %q", resp.StatusCode, company)
	}

	var postings []leverPosting
	if err := json.NewDecoder(resp.Body).Decode(&postings); err != nil {
		return nil, fmt.Errorf("decoding lever response: %w", err)
	}

	jobs := make([]model.Job, 0, len(postings))
	for _, p := range postings {
		jobs = append(jobs, model.Job{
			Title:    p.Text,
			Location: p.Categories.Location,
			URL:      p.HostedURL,
		})
	}
	return jobs, nil
}
