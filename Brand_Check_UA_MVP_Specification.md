# Brand Check UA — MVP Specification

## Goal

An iOS application that allows users to:

1. Search for a company or brand.
2. Scan a product barcode.
3. Determine:
   - parent company;
   - whether the company continues operating in Russia;
   - whether the company appears in Ukrainian sanctions lists;
   - sources supporting the information.

The application must work offline after downloading the latest dataset.

---

## Legal Considerations

The architecture itself is legal, but every imported dataset must be reviewed for licensing and attribution requirements.

The application should avoid making legal conclusions such as:
- "supports terrorism"

Instead present factual statements such as:
- "Subject to Ukrainian sanctions"
- "Listed in the Leave Russia dataset as continuing operations in Russia"
- "Source: OpenSanctions"
- "Source: KSE Leave Russia"

All displayed information should include source attribution and update timestamps.

---

# Architecture

GitHub Actions
→ Data Aggregation Pipeline
→ Static JSON Dataset
→ GitHub Pages
→ iOS Application

No backend server.

No database server.

No user accounts.

---

# Data Sources

## Source 1 — Ukrainian Sanctions

Official sanctions lists.

Stored fields:

{
  "entity_name": "",
  "sanctioned": true,
  "sanction_source": "",
  "sanction_date": ""
}

---

## Source 2 — OpenSanctions

Used for:
- company aliases
- alternative spellings
- matching
- sanctions enrichment

Stored fields:

{
  "entity_name": "",
  "aliases": [],
  "country": "",
  "opensanctions_id": ""
}

---

## Source 3 — Leave Russia Dataset

Primary source for Russia status.

Stored fields:

{
  "company_name": "",
  "status": "",
  "last_updated": ""
}

Supported statuses:
- Exited
- Suspended
- Reduced Operations
- Operating
- Unknown

---

## Source 4 — Brand Ownership Dataset

Maintained in repository.

Example:

{
  "brand": "Oreo",
  "owner": "Mondelez International"
}

Purpose:
Users search brands rather than corporations.

---

# Repository Structure

repository/

├── app/
│   └── iOS application
│
├── data/
│   ├── sanctions/
│   ├── opensanctions/
│   ├── leave-russia/
│   └── brands/
│
├── scripts/
│   ├── import
│   ├── merge
│   └── export
│
├── output/
│   └── companies.json.gz
│
└── .github/workflows/
    └── update.yml

---

# Dataset Format

Generated file:

companies.json.gz

Example:

{
  "companies": [
    {
      "id": "mondelez",
      "name": "Mondelez International",
      "aliases": [],
      "russia_status": "Operating",
      "sanctioned_ua": false,
      "brands": [
        "Oreo",
        "Milka",
        "Toblerone"
      ],
      "sources": [
        "KSE",
        "OpenSanctions"
      ]
    }
  ]
}

---

# GitHub Actions

Schedule:

cron: "0 2 * * *"

Daily execution:

1. Download latest source data.
2. Normalize company names.
3. Match entities.
4. Generate dataset.
5. Compress JSON.
6. Publish to GitHub Pages.

---

# GitHub Pages

Public endpoint:

https://<org>.github.io/<repo>/companies.json.gz

Version endpoint:

{
  "version": "2026-06-01",
  "records": 14321
}

---

# iOS MVP Features

## Search

Search by:
- brand
- company

Results update as user types.

---

## Company Details

Display:

Company Name

Status:
Operating in Russia

Ukraine Sanctions:
Yes

Brands:
Brand A
Brand B
Brand C

Sources:
KSE
OpenSanctions

---

## Barcode Scanner

Flow:

Scan barcode
→ Lookup brand
→ Lookup company
→ Show result

Initial implementation can use a local barcode database subset.

If barcode mapping is unavailable:

Product not found → Search manually

---

## Offline Mode

Application stores data in SQLite.

Search works without internet.

Internet only required for dataset updates.

---

# Update Strategy

On launch:

Check version.json

If newer version exists:

Download companies.json.gz
Replace local database

---

# Non-Goals (MVP)

Not included:

- User accounts
- Comments
- Ratings
- Social features
- AI summaries
- Push notifications
- Crowdsourced reports

---

# Suggested Tech Stack

## Data Pipeline

- Go
- SQLite
- GitHub Actions

## Hosting

- GitHub Pages

## Mobile

- SwiftUI
- SQLite
- AVFoundation (barcode scanning)

---

# Future Features

- Product photos
- Company ownership graphs
- Favorites
- Watchlist
- Widgets
- Shortcuts integration
- Apple Watch companion
