# Scrape Hacker News

**Runtime:** claude · **RAM:** 2048 MB · **Expected duration:** 40–60 s · **Cost:** ~$0.15–0.30

Demonstrates: Python + requests + BeautifulSoup, JSON structured output, Claude reasoning about pagination.

## Prompt

```prompt
Fetch the front page of https://news.ycombinator.com and extract all stories:
rank, title, url, points, author, comment_count, age. Output the list as
/root/output/top-stories.json (one array of objects). Also write a brief
/root/output/summary.md with the top 5 titles and any patterns you notice
about what's trending today.
```

## Expected output files

- `top-stories.json` — ~30 entries, one per front-page story
- `summary.md` — qualitative Claude analysis
