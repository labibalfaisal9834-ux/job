package model

// Company is one entry from companies.json.
type Company struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Job is a single open position extracted from a career page.
type Job struct {
	Title    string `json:"title"`
	Location string `json:"location"`
	URL      string `json:"url"`
}

// FoundJob pairs a job with the company it came from, for notification purposes.
type FoundJob struct {
	Company string
	Job     Job
}
