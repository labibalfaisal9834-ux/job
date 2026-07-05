package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"jobwatch/internal/model"
)

const apiBase = "https://generativelanguage.googleapis.com/v1beta/models"

// QuotaError is returned when Gemini responds with HTTP 429. DailyExhausted
// distinguishes an unrecoverable-within-this-run daily cap (RPD) from a
// short per-minute limit (RPM/TPM) that's worth retrying after RetryAfter.
type QuotaError struct {
	DailyExhausted bool
	RetryAfter     time.Duration
	raw            string
}

func (e *QuotaError) Error() string {
	if e.DailyExhausted {
		return "gemini daily quota exhausted"
	}
	return fmt.Sprintf("gemini rate limited, retry after %s", e.RetryAfter)
}

type Client struct {
	APIKey string
	Model  string // e.g. "gemini-2.5-flash" - check ai.google.dev for current free-tier model names
	HTTP   *http.Client
}

func New(apiKey, model string) *Client {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &Client{
		APIKey: apiKey,
		Model:  model,
		HTTP:   &http.Client{Timeout: 60 * time.Second},
	}
}

type genRequest struct {
	Contents []content `json:"contents"`
	GenerationConfig struct {
		ResponseMimeType string `json:"responseMimeType"`
	} `json:"generationConfig"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type genResponse struct {
	Candidates []struct {
		Content content `json:"content"`
	} `json:"candidates"`
}

// ExtractJobs sends the cleaned page text to Gemini and asks it to return a
// JSON array of open positions. Returns a *QuotaError (unwrap with
// errors.As) if rate limited or out of daily quota.
func (c *Client) ExtractJobs(companyName, pageText string) ([]model.Job, error) {
	if strings.TrimSpace(pageText) == "" {
		return nil, nil
	}

	prompt := buildPrompt(companyName, pageText)

	reqBody := genRequest{Contents: []content{{Parts: []part{{Text: prompt}}}}}
	reqBody.GenerationConfig.ResponseMimeType = "application/json"

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling gemini request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", apiBase, c.Model, c.APIKey)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling gemini api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading gemini response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, parseQuotaError(body)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini api returned status %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var parsed genResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding gemini response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, nil
	}

	text := parsed.Candidates[0].Content.Parts[0].Text
	var jobs []model.Job
	if err := json.Unmarshal([]byte(text), &jobs); err != nil {
		return nil, fmt.Errorf("gemini did not return valid job JSON: %w (raw: %s)", err, truncate(text, 300))
	}
	return jobs, nil
}

func buildPrompt(companyName, pageText string) string {
	return fmt.Sprintf(`You are given the visible text content of %s's careers/jobs page. Links appear inline in the form "link text [https://absolute-url]".

Extract every currently open job position mentioned. Return ONLY a JSON array (no markdown, no commentary), where each element has exactly these keys:
- "title": the job title, as written
- "location": the location if mentioned, otherwise an empty string
- "url": the absolute URL to that specific job posting if one is present in the text, otherwise an empty string

If there are no open positions, return an empty array: []

Page text:
"""
%s
"""`, companyName, pageText)
}

// parseQuotaError inspects a 429 response body to tell a daily quota
// exhaustion (RPD, only clears after Google's midnight-Pacific reset) apart
// from a short per-minute limit (RPM/TPM, clears in seconds).
func parseQuotaError(body []byte) *QuotaError {
	raw := string(body)

	// Google's QuotaFailure detail includes a quotaId like
	// "GenerateRequestsPerDayPerProjectPerModel-FreeTier" for daily caps,
	// vs "...PerMinute..." for short-lived ones.
	if strings.Contains(raw, "PerDay") {
		return &QuotaError{DailyExhausted: true, raw: raw}
	}

	retryAfter := 20 * time.Second // sensible default if Google doesn't tell us
	if idx := strings.Index(raw, `"retryDelay"`); idx != -1 {
		// crude extraction of the quoted duration value, e.g. "retryDelay":"18s"
		rest := raw[idx:]
		if start := strings.Index(rest, `:`); start != -1 {
			rest = rest[start+1:]
			rest = strings.TrimSpace(rest)
			rest = strings.TrimPrefix(rest, `"`)
			if end := strings.Index(rest, `"`); end != -1 {
				if d, err := time.ParseDuration(rest[:end]); err == nil {
					retryAfter = d
				}
			}
		}
	}

	return &QuotaError{DailyExhausted: false, RetryAfter: retryAfter, raw: raw}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
