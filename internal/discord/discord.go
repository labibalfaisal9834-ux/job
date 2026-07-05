package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"jobwatch/internal/model"
)

const maxMessageLen = 1900 // stay under Discord's 2000 char content limit

// Notify posts newly found jobs to a Discord webhook, splitting into
// multiple messages if the list is long.
func Notify(webhookURL string, jobs []model.FoundJob) error {
	if len(jobs) == 0 {
		return nil
	}

	// Group by company for readability.
	byCompany := map[string][]model.Job{}
	var order []string
	for _, fj := range jobs {
		if _, seen := byCompany[fj.Company]; !seen {
			order = append(order, fj.Company)
		}
		byCompany[fj.Company] = append(byCompany[fj.Company], fj.Job)
	}

	var chunks []string
	var current strings.Builder
	current.WriteString(fmt.Sprintf("**New job postings found (%d):**\n", len(jobs)))

	for _, company := range order {
		block := formatCompanyBlock(company, byCompany[company])
		if current.Len()+len(block) > maxMessageLen {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		current.WriteString(block)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	for i, chunk := range chunks {
		if err := send(webhookURL, chunk); err != nil {
			return fmt.Errorf("sending discord message %d/%d: %w", i+1, len(chunks), err)
		}
		time.Sleep(500 * time.Millisecond) // stay well under Discord's webhook rate limit
	}
	return nil
}

func formatCompanyBlock(company string, jobs []model.Job) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n**%s**\n", company))
	for _, j := range jobs {
		loc := j.Location
		if loc == "" {
			loc = "location not specified"
		}
		if j.URL != "" {
			sb.WriteString(fmt.Sprintf("• %s (%s) — %s\n", j.Title, loc, j.URL))
		} else {
			sb.WriteString(fmt.Sprintf("• %s (%s)\n", j.Title, loc))
		}
	}
	return sb.String()
}

func send(webhookURL, content string) error {
	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}
	return nil
}
