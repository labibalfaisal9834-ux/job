package companies

import (
	"encoding/json"
	"fmt"
	"os"

	"jobwatch/internal/model"
)

// Load reads the list of companies (name + career page URL) from a JSON file.
//
// Expected format:
//
//	[
//	  {"name": "Acme Inc", "url": "https://boards.greenhouse.io/acme"},
//	  {"name": "Widgets Co", "url": "https://jobs.lever.co/widgetsco"},
//	  {"name": "Some Startup", "url": "https://somestartup.com/careers"}
//	]
func Load(path string) ([]model.Company, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading companies file %q: %w", path, err)
	}

	var list []model.Company
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parsing companies file %q: %w", path, err)
	}

	for i, c := range list {
		if c.Name == "" {
			return nil, fmt.Errorf("company at index %d is missing a \"name\"", i)
		}
		if c.URL == "" {
			return nil, fmt.Errorf("company %q is missing a \"url\"", c.Name)
		}
	}

	return list, nil
}
