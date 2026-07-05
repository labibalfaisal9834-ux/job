// Command jobwatch checks a list of company career pages for open
// positions and posts new ones to a Discord webhook.
//
// Designed to be run on a schedule (e.g. every 6 hours via GitHub Actions).
// Each run decides for itself what to do:
//
//  1. If a previous run left companies unprocessed (because Gemini's daily
//     quota ran out), resume exactly where it left off.
//  2. Otherwise, if BATCH_INTERVAL_DAYS (default 4) have passed since the
//     last full sweep, start a new one covering every company.
//  3. Otherwise, there's nothing to do yet — exit immediately.
//
// State (last batch start time, pending companies, already-seen jobs) is
// persisted to STATE_FILE (default state.json), which the calling GitHub
// Actions workflow commits back to the repo after every run.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"jobwatch/internal/ats"
	"jobwatch/internal/companies"
	"jobwatch/internal/discord"
	"jobwatch/internal/fetch"
	"jobwatch/internal/gemini"
	"jobwatch/internal/model"
	"jobwatch/internal/state"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("jobwatch: %v", err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	companyList, err := companies.Load(cfg.companiesFile)
	if err != nil {
		return err
	}
	byName := map[string]model.Company{}
	for _, c := range companyList {
		byName[c.Name] = c
	}

	st, err := state.Load(cfg.stateFile)
	if err != nil {
		return err
	}

	toProcess, isResume := decideBatch(st, cfg.batchInterval, companyList)
	if toProcess == nil {
		next := st.LastBatchStart.Add(cfg.batchInterval)
		log.Printf("nothing to do yet — next full batch starts around %s", next.Format(time.RFC3339))
		return nil
	}
	if isResume {
		log.Printf("resuming previous batch: %d companies remaining", len(toProcess))
	} else {
		log.Printf("starting new batch: %d companies", len(toProcess))
		st.LastBatchStart = time.Now().UTC()
	}

	geminiClient := gemini.New(cfg.geminiAPIKey, cfg.geminiModel)

	var newJobs []model.FoundJob
	var remaining []string
	dailyQuotaHit := false

	for i, name := range toProcess {
		if dailyQuotaHit {
			remaining = append(remaining, toProcess[i:]...)
			break
		}

		company, ok := byName[name]
		if !ok {
			log.Printf("skipping %q: no longer in companies file", name)
			continue
		}

		jobs, err := processCompany(geminiClient, company)
		if err != nil {
			var qerr *gemini.QuotaError
			if errors.As(err, &qerr) && qerr.DailyExhausted {
				log.Printf("gemini daily quota exhausted while processing %q — stopping, will resume next run", company.Name)
				remaining = append(remaining, toProcess[i:]...)
				dailyQuotaHit = true
				continue
			}
			// Any other error (page fetch failure, malformed response, a
			// per-minute limit we couldn't clear after retrying, etc.) -
			// log it and move on to the next company rather than blocking
			// the whole batch.
			log.Printf("error processing %q: %v", company.Name, err)
			continue
		}

		for _, j := range jobs {
			if j.Title == "" {
				continue
			}
			key := state.JobKey(company.Name, j.Title, j.URL)
			if st.SeenJobs[key] {
				continue
			}
			st.SeenJobs[key] = true
			newJobs = append(newJobs, model.FoundJob{Company: company.Name, Job: j})
		}
	}

	st.Pending = remaining
	if err := st.Save(cfg.stateFile); err != nil {
		return err
	}

	log.Printf("run complete: %d new job(s) found, %d company(ies) pending for next run", len(newJobs), len(remaining))

	if len(newJobs) > 0 {
		if err := discord.Notify(cfg.discordWebhook, newJobs); err != nil {
			return fmt.Errorf("notifying discord: %w", err)
		}
	}

	return nil
}

// processCompany fetches a company's postings, using a direct ATS API when
// possible and falling back to HTML + Gemini otherwise. It retries a bounded
// number of times on short (per-minute) Gemini rate limits before giving up
// on this company for the current run.
func processCompany(client *gemini.Client, company model.Company) ([]model.Job, error) {
	if token, ok := ats.DetectGreenhouse(company.URL); ok {
		log.Printf("%s: using Greenhouse API (token=%s)", company.Name, token)
		return ats.FetchGreenhouseJobs(token)
	}
	if slug, ok := ats.DetectLever(company.URL); ok {
		log.Printf("%s: using Lever API (company=%s)", company.Name, slug)
		return ats.FetchLeverJobs(slug)
	}

	log.Printf("%s: fetching and asking Gemini to extract jobs", company.Name)
	text, err := fetch.PageText(company.URL)
	if err != nil {
		return nil, fmt.Errorf("fetching page: %w", err)
	}

	const maxRetries = 4
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		jobs, err := client.ExtractJobs(company.Name, text)
		if err == nil {
			return jobs, nil
		}

		var qerr *gemini.QuotaError
		if errors.As(err, &qerr) {
			if qerr.DailyExhausted {
				return nil, err // bubble up untouched so caller can stop the whole batch
			}
			log.Printf("%s: rate limited, waiting %s before retry (%d/%d)", company.Name, qerr.RetryAfter, attempt+1, maxRetries)
			time.Sleep(qerr.RetryAfter)
			lastErr = err
			continue
		}

		return nil, err
	}
	return nil, fmt.Errorf("gave up after %d retries: %w", maxRetries, lastErr)
}

// decideBatch figures out which companies (if any) should be processed this
// run, and whether that's a resume of a prior interrupted batch.
func decideBatch(st *state.State, interval time.Duration, all []model.Company) (names []string, isResume bool) {
	if len(st.Pending) > 0 {
		return st.Pending, true
	}

	if st.LastBatchStart.IsZero() || time.Since(st.LastBatchStart) >= interval {
		names = make([]string, 0, len(all))
		for _, c := range all {
			names = append(names, c.Name)
		}
		return names, false
	}

	return nil, false
}

type config struct {
	geminiAPIKey   string
	geminiModel    string
	discordWebhook string
	companiesFile  string
	stateFile      string
	batchInterval  time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		geminiAPIKey:   os.Getenv("GEMINI_API_KEY"),
		geminiModel:    envOr("GEMINI_MODEL", "gemini-2.5-flash"),
		discordWebhook: os.Getenv("DISCORD_WEBHOOK_URL"),
		companiesFile:  envOr("COMPANIES_FILE", "companies.json"),
		stateFile:      envOr("STATE_FILE", "state.json"),
	}

	if cfg.geminiAPIKey == "" {
		return cfg, errors.New("GEMINI_API_KEY environment variable is required")
	}
	if cfg.discordWebhook == "" {
		return cfg, errors.New("DISCORD_WEBHOOK_URL environment variable is required")
	}

	days := 4
	if v := os.Getenv("BATCH_INTERVAL_DAYS"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("invalid BATCH_INTERVAL_DAYS %q: %w", v, err)
		}
		days = parsed
	}
	cfg.batchInterval = time.Duration(days) * 24 * time.Hour

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
