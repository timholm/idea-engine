package research

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/timholm/idea-engine/internal/config"
	"github.com/timholm/idea-engine/internal/types"
)

const maxRelatedRepos = 7

// RepoResearcher finds related GitHub repos for a candidate paper.
type RepoResearcher struct {
	cfg    *config.Config
	client *http.Client
}

// NewRepoResearcher creates a RepoResearcher.
func NewRepoResearcher(cfg *config.Config) *RepoResearcher {
	return &RepoResearcher{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FindRelatedRepos finds the top 7 GitHub repos related to a candidate paper.
func (rr *RepoResearcher) FindRelatedRepos(title, abstract, fullText string) ([]types.RepoSummary, error) {
	seen := make(map[string]bool)
	var scored []scoredRepo

	// Strategy 1: Search by paper title keywords
	titleQuery := extractSearchQuery(title)
	if titleQuery != "" {
		repos, err := rr.searchGitHub(titleQuery)
		if err != nil {
			log.Printf("[research/repos] warning: title search failed: %v", err)
		} else {
			for _, r := range repos {
				if seen[r.HTMLURL] {
					continue
				}
				seen[r.HTMLURL] = true
				scored = append(scored, scoredRepo{repo: r, relevance: 0.9})
			}
		}
	}

	// Strategy 2: Search by technique name (extracted from abstract)
	techniques := extractTechniques(abstract)
	for _, tech := range techniques {
		repos, err := rr.searchGitHub(tech)
		if err != nil {
			log.Printf("[research/repos] warning: technique search '%s' failed: %v", tech, err)
			continue
		}
		for _, r := range repos {
			if seen[r.HTMLURL] {
				continue
			}
			seen[r.HTMLURL] = true
			scored = append(scored, scoredRepo{repo: r, relevance: 0.7})
		}
	}

	// Strategy 3: Extract GitHub URLs mentioned in the paper text
	if fullText != "" {
		urls := extractGitHubURLs(fullText)
		for _, ghURL := range urls {
			if seen[ghURL] {
				continue
			}
			seen[ghURL] = true
			// Fetch repo details from the URL
			repo, err := rr.fetchRepo(ghURL)
			if err != nil {
				log.Printf("[research/repos] warning: failed to fetch %s: %v", ghURL, err)
				continue
			}
			scored = append(scored, scoredRepo{repo: *repo, relevance: 1.0}) // directly cited = highest relevance
		}
	}

	// Filter: stars > 10, updated in last 12 months, not archived
	cutoff := time.Now().AddDate(-1, 0, 0)
	var filtered []scoredRepo
	for _, sr := range scored {
		if sr.repo.Stars < 10 {
			continue
		}
		if sr.repo.Archived {
			continue
		}
		pushed, err := time.Parse(time.RFC3339, sr.repo.PushedAt)
		if err == nil && pushed.Before(cutoff) {
			continue
		}
		filtered = append(filtered, sr)
	}

	// Sort by combined relevance + star score
	sort.Slice(filtered, func(i, j int) bool {
		si := filtered[i].relevance + normalizeStars(filtered[i].repo.Stars)
		sj := filtered[j].relevance + normalizeStars(filtered[j].repo.Stars)
		return si > sj
	})

	// Take top 7 and fetch README + file tree for each
	limit := maxRelatedRepos
	if len(filtered) < limit {
		limit = len(filtered)
	}

	results := make([]types.RepoSummary, 0, limit)
	for _, sr := range filtered[:limit] {
		summary := types.RepoSummary{
			URL:         sr.repo.HTMLURL,
			Name:        sr.repo.FullName,
			Description: sr.repo.Description,
			Stars:       sr.repo.Stars,
			Language:    sr.repo.Language,
			Relevance:   sr.relevance,
		}

		// Fetch README
		readme, err := rr.fetchREADME(sr.repo.FullName)
		if err != nil {
			log.Printf("[research/repos] warning: failed to fetch README for %s: %v", sr.repo.FullName, err)
		} else {
			// Truncate to first 2000 chars to keep context manageable
			if len(readme) > 2000 {
				readme = readme[:2000] + "..."
			}
			summary.README = readme
		}

		// Fetch file tree (top-level only)
		tree, err := rr.fetchFileTree(sr.repo.FullName)
		if err != nil {
			log.Printf("[research/repos] warning: failed to fetch file tree for %s: %v", sr.repo.FullName, err)
		} else {
			summary.FileTree = tree
		}

		results = append(results, summary)
	}

	log.Printf("[research/repos] found %d related repos (from %d candidates, %d after filter)", len(results), len(scored), len(filtered))
	return results, nil
}

type scoredRepo struct {
	repo      types.GitHubRepo
	relevance float64
}

func (rr *RepoResearcher) searchGitHub(query string) ([]types.GitHubRepo, error) {
	u := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=stars&order=desc&per_page=15",
		url.QueryEscape(query))

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+rr.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := rr.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub search returned %d: %s", resp.StatusCode, string(body))
	}

	var result types.GitHubSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding GitHub search: %w", err)
	}
	return result.Items, nil
}

func (rr *RepoResearcher) fetchRepo(htmlURL string) (*types.GitHubRepo, error) {
	// Extract owner/repo from URL: https://github.com/owner/repo
	parts := strings.Split(strings.TrimPrefix(htmlURL, "https://github.com/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid GitHub URL: %s", htmlURL)
	}
	owner, repo := parts[0], parts[1]

	u := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+rr.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := rr.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching repo %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub repo %s/%s returned %d: %s", owner, repo, resp.StatusCode, string(body))
	}

	var ghRepo types.GitHubRepo
	if err := json.NewDecoder(resp.Body).Decode(&ghRepo); err != nil {
		return nil, err
	}
	return &ghRepo, nil
}

func (rr *RepoResearcher) fetchREADME(fullName string) (string, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/readme", fullName)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+rr.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := rr.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("README fetch returned %d", resp.StatusCode)
	}

	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&content); err != nil {
		return "", err
	}

	if content.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
		if err != nil {
			return "", fmt.Errorf("decoding base64 README: %w", err)
		}
		return string(decoded), nil
	}

	return content.Content, nil
}

func (rr *RepoResearcher) fetchFileTree(fullName string) ([]string, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/contents/", fullName)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+rr.cfg.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := rr.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file tree fetch returned %d", resp.StatusCode)
	}

	var contents []types.GitHubContent
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}

	var tree []string
	for _, c := range contents {
		prefix := "  "
		if c.Type == "dir" {
			prefix = "d "
		}
		tree = append(tree, prefix+c.Path)
	}
	return tree, nil
}

// extractSearchQuery extracts meaningful keywords from a paper title for GitHub search.
func extractSearchQuery(title string) string {
	// Remove common academic filler words
	stopWords := map[string]bool{
		"a": true, "an": true, "the": true, "of": true, "for": true,
		"and": true, "or": true, "in": true, "on": true, "with": true,
		"to": true, "from": true, "by": true, "is": true, "are": true,
		"via": true, "using": true, "based": true, "towards": true,
		"new": true, "novel": true, "approach": true, "method": true,
	}

	words := strings.Fields(strings.ToLower(title))
	var keywords []string
	for _, w := range words {
		// Strip punctuation
		w = strings.Trim(w, ".,;:!?()-[]{}\"'")
		if w == "" || stopWords[w] || len(w) <= 2 {
			continue
		}
		keywords = append(keywords, w)
	}

	if len(keywords) > 5 {
		keywords = keywords[:5] // GitHub search works best with fewer terms
	}

	return strings.Join(keywords, " ")
}

// extractTechniques identifies technique names from an abstract for targeted repo search.
func extractTechniques(abstract string) []string {
	if abstract == "" {
		return nil
	}

	// Look for phrases that typically name techniques
	patterns := []string{
		`(?i)(?:we (?:propose|introduce|present) )([A-Z][A-Za-z\-]+(?:\s+[A-Z][A-Za-z\-]+)*)`,
		`(?i)(?:called |named )([A-Z][A-Za-z\-]+(?:\s+[A-Z][A-Za-z\-]+)*)`,
		`(?i)\(([A-Z]{2,}[A-Za-z]*)\)`, // acronyms in parentheses
	}

	seen := make(map[string]bool)
	var techniques []string

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(abstract, -1)
		for _, m := range matches {
			if len(m) > 1 {
				tech := strings.TrimSpace(m[1])
				lower := strings.ToLower(tech)
				if !seen[lower] && len(tech) > 2 {
					seen[lower] = true
					techniques = append(techniques, tech)
				}
			}
		}
	}

	// Limit to top 3 technique searches to avoid rate limiting
	if len(techniques) > 3 {
		techniques = techniques[:3]
	}

	return techniques
}

// extractGitHubURLs finds GitHub repository URLs in text.
func extractGitHubURLs(text string) []string {
	re := regexp.MustCompile(`https?://github\.com/([a-zA-Z0-9\-]+/[a-zA-Z0-9\-_.]+)`)
	matches := re.FindAllStringSubmatch(text, -1)

	seen := make(map[string]bool)
	var urls []string
	for _, m := range matches {
		fullURL := "https://github.com/" + m[1]
		// Strip trailing punctuation or .git
		fullURL = strings.TrimSuffix(fullURL, ".git")
		fullURL = strings.TrimSuffix(fullURL, ".")
		if !seen[fullURL] {
			seen[fullURL] = true
			urls = append(urls, fullURL)
		}
	}
	return urls
}

// normalizeStars converts star count to a 0-1 score. Uses log scale.
func normalizeStars(stars int) float64 {
	if stars <= 0 {
		return 0
	}
	// log10(10) = 1, log10(100) = 2, log10(1000) = 3, log10(10000) = 4
	// Normalize to 0-0.5 range
	score := 0.0
	switch {
	case stars >= 10000:
		score = 0.5
	case stars >= 1000:
		score = 0.4
	case stars >= 100:
		score = 0.3
	case stars >= 50:
		score = 0.2
	default:
		score = 0.1
	}
	return score
}
