# jobwatch

Watches a list of company career pages for open positions and posts new ones
to a Discord channel. Runs entirely on GitHub Actions' free tier — no VPS,
no credit card, nothing to keep running 24/7 yourself.

## How it decides what to do each run

The workflow triggers every 6 hours, but most of those runs will do nothing.
Each run, the program checks:

1. **Is there a leftover batch from last time?** (i.e. a previous run got
   cut off by Gemini's *daily* quota mid-sweep) → resume exactly where it
   left off.
2. **Otherwise, has it been ≥4 days since the last full sweep?** → start a
   fresh batch covering every company in `companies.json`.
3. **Otherwise** → nothing to do, exit immediately.

So in the common case, a full sweep finishes within one 6-hour-cycle run and
you get one check every 4 days as intended. The 6-hour cron only matters
when a sweep gets interrupted by a daily quota limit — it retries sooner
instead of waiting a full 4 days for the next scheduled batch.

## How each company is checked

- If the URL is a **Greenhouse** (`boards.greenhouse.io/...`) or **Lever**
  (`jobs.lever.co/...`) page, jobwatch calls their public JSON APIs
  directly. No AI involved, no quota used, and it's more reliable than
  scraping.
- For everything else, jobwatch downloads the page's HTML, strips it down
  to plain text (keeping links inline), and asks Gemini to extract a JSON
  list of open positions.

## ⚠️ Important limitation: JavaScript-rendered pages

jobwatch does a plain HTTP GET — it does **not** run JavaScript. If a
career page loads its job list via client-side JS (common with some Workday
instances or custom React/Vue sites), the page will look empty to Gemini and
you'll get zero results for that company, with no error. Greenhouse and
Lever are unaffected since those go through the direct API path. If you
notice a company never turns up any jobs, check whether its page needs
JavaScript to show listings — that's the likely cause.

## Setup

### 1. Get a Gemini API key

Free, no credit card, from [Google AI Studio](https://aistudio.google.com/app/apikey).

Gemini's free-tier model names and limits change fairly often — check
[ai.google.dev/gemini-api/docs/rate-limits](https://ai.google.dev/gemini-api/docs/rate-limits)
for the current recommended free model, and update `GEMINI_MODEL` below if
needed. As of writing, `gemini-2.5-flash` is a solid free-tier default (set
in code, no need to configure unless you want to change it).

### 2. Create a Discord webhook

In your Discord server: **Channel Settings → Integrations → Webhooks → New
Webhook**. Copy the webhook URL.

### 3. Add repo secrets

In your GitHub repo: **Settings → Secrets and variables → Actions → New
repository secret**. Add:

- `GEMINI_API_KEY`
- `DISCORD_WEBHOOK_URL`

### 4. Edit `companies.json`

Replace the sample entries with your actual list:

```json
[
  { "name": "Company A", "url": "https://boards.greenhouse.io/companya" },
  { "name": "Company B", "url": "https://jobs.lever.co/companyb" },
  { "name": "Company C", "url": "https://companyc.com/careers" }
]
```

### 5. Enable Actions write permissions

**Settings → Actions → General → Workflow permissions** → select
**"Read and write permissions"**. This lets the workflow commit the updated
`state.json` back to the repo after each run.

### 6. Push and wait, or trigger manually

Once pushed, the workflow starts running on its 6-hour cron automatically.
To test immediately instead of waiting: go to the **Actions** tab →
**Job Watch** → **Run workflow**.

## Configuration knobs

Set these as extra repo secrets/variables and uncomment the corresponding
lines in `.github/workflows/jobwatch.yml` if you want to change them:

| Variable | Default | Meaning |
|---|---|---|
| `GEMINI_MODEL` | `gemini-2.5-flash` | Which Gemini model to call |
| `BATCH_INTERVAL_DAYS` | `4` | Days between full sweeps |

## A couple of honest caveats

- **GitHub's free cron isn't guaranteed to run exactly on time** — GitHub
  explicitly says scheduled workflows can be delayed during high load,
  especially in busy periods. Don't rely on it for anything time-critical.
- **`state.json` grows slowly forever** (one entry per job ever seen, so
  you're never re-notified). For a personal list of a few dozen companies
  this is a non-issue; it's not pruned automatically.
- Gemini's free-tier model lineup and limits have shifted multiple times
  this year — if extraction suddenly starts failing, it's worth checking
  whether Google renamed or retired the model you're using.

## Running locally (for testing)

```bash
export GEMINI_API_KEY=your_key
export DISCORD_WEBHOOK_URL=your_webhook
go run ./cmd/jobwatch
```

This will read/write `companies.json` and `state.json` from the current
directory, same as in CI.
