

// rss.go Package main implements a high-performance RSS reader with ordered storage
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	
	"context"
	
	"github.com/peterbourgon/ff/v3"
	flag "github.com/spf13/pflag"
	"golang.org/x/net/html"
)

// Config holds application configuration
type Config struct {
	Feeds      []string
	Limit      int
	Output     string
	Since      time.Duration
	MaxPerFeed int
	NoCache    bool
	Update     bool
	Purge      bool
	DataDir    string
	Format     string
	Reverse    bool
}

// FeedItem represents a single RSS item
type FeedItem struct {
	Feed      string    `json:"feed"`
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	Published time.Time `json:"published"`
	Added     time.Time `json:"added"`
	ID        string    `json:"id"`
	Read      bool      `json:"read"`
	Starred   bool      `json:"starred"`
}

// FeedStore manages feed storage
type FeedStore struct {
	items     []FeedItem
	mu        sync.RWMutex
	path      string
	maxItems  int
	lastFetch map[string]time.Time
}

// NewFeedStore creates a new feed store
func NewFeedStore(path string, maxItems int) (*FeedStore, error) {
	s := &FeedStore{
		path:      path,
		maxItems:  maxItems,
		lastFetch: make(map[string]time.Time),
	}
	
	if err := s.load(); err != nil {
		return nil, err
	}
	
	return s, nil
}

// Add adds items maintaining chronological order
func (s *FeedStore) Add(items []FeedItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// Remove duplicates by ID
	existing := make(map[string]bool)
	for _, item := range s.items {
		existing[item.ID] = true
	}
	
	// Add new items
	for _, item := range items {
		if existing[item.ID] {
			continue
		}
		s.items = append(s.items, item)
		existing[item.ID] = true
	}
	
	// Sort by published date (oldest first)
	sort.SliceStable(s.items, func(i, j int) bool {
		return s.items[i].Published.Before(s.items[j].Published)
	})
	
	// Limit per feed
	s.truncatePerFeed()
	
	// Keep only latest items overall
	if len(s.items) > s.maxItems*len(s.uniqueFeeds()) {
		s.items = s.items[len(s.items)-s.maxItems*len(s.uniqueFeeds()):]
	}
	
	return s.save()
}

// List returns items with optional filtering
func (s *FeedStore) List(limit int, feedFilter string, since time.Time, reverse bool) []FeedItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	var items []FeedItem
	now := time.Now()
	
	// Apply filters
	for _, item := range s.items {
		// Filter by feed
		if feedFilter != "" && !strings.Contains(item.Feed, feedFilter) {
			continue
		}
		// Filter by date
		if !since.IsZero() && item.Published.Before(since) {
			continue
		}
		items = append(items, item)
	}
	
	// Apply ordering
	if reverse {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Published.After(items[j].Published)
		})
	} else {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Published.Before(items[j].Published)
		})
	}
	
	// Apply limit
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	
	return items
}

// UpdateFeed updates a specific feed
func (s *FeedStore) UpdateFeed(ctx context.Context, url string) (int, error) {
	items, err := fetchFeed(ctx, url)
	if err != nil {
		return 0, err
	}
	
	if err := s.Add(items); err != nil {
		return 0, err
	}
	
	s.lastFetch[url] = time.Now()
	return len(items), nil
}

// truncatePerFeed keeps only latest items per feed
func (s *FeedStore) truncatePerFeed() {
	feedCount := make(map[string]int)
	feedItems := make(map[string][]FeedItem)
	
	// Group by feed
	for _, item := range s.items {
		feedItems[item.Feed] = append(feedItems[item.Feed], item)
	}
	
	// Keep only latest items per feed
	var newItems []FeedItem
	for feed, items := range feedItems {
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Published.Before(items[j].Published)
		})
		
		// Keep only latest per feed
		if len(items) > s.maxItems {
			items = items[len(items)-s.maxItems:]
		}
		
		newItems = append(newItems, items...)
		feedCount[feed] = len(items)
	}
	
	// Sort all items chronologically
	sort.SliceStable(newItems, func(i, j int) bool {
		return newItems[i].Published.Before(newItems[j].Published)
	})
	
	s.items = newItems
}

// uniqueFeeds returns unique feed names
func (s *FeedStore) uniqueFeeds() map[string]bool {
	feeds := make(map[string]bool)
	for _, item := range s.items {
		feeds[item.Feed] = true
	}
	return feeds
}

// load loads items from disk
func (s *FeedStore) load() error {
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return nil
	}
	
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	
	var items []FeedItem
	if err := json.Unmarshal(data, &items); err != nil {
		// Try legacy format
		return nil
	}
	
	s.mu.Lock()
	s.items = items
	s.mu.Unlock()
	
	return nil
}

// save saves items to disk
func (s *FeedStore) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.items, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	
	// Write atomically
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// Fetcher handles concurrent feed fetching
type Fetcher struct {
	store  *FeedStore
	client *http.Client
	sem    chan struct{}
	mu     sync.Mutex
	stats  map[string]int
}

// NewFetcher creates a new fetcher
func NewFetcher(store *FeedStore) *Fetcher {
	return &Fetcher{
		store: store,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		sem:   make(chan struct{}, 5), // Limit concurrent fetches
		stats: make(map[string]int),
	}
}

// FetchAll fetches all feeds concurrently
func (f *Fetcher) FetchAll(ctx context.Context, urls []string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(urls))
	
	for _, url := range urls {
		wg.Add(1)
		
		go func(u string) {
			defer wg.Done()
			
			// Acquire semaphore
			select {
			case f.sem <- struct{}{}:
				defer func() { <-f.sem }()
			case <-ctx.Done():
				return
			}
			
			// Fetch feed
			count, err := f.store.UpdateFeed(ctx, u)
			if err != nil {
				errs <- fmt.Errorf("%s: %w", u, err)
				return
			}
			
			f.mu.Lock()
			f.stats[u] = count
			f.mu.Unlock()
		}(url)
	}
	
	wg.Wait()
	close(errs)
	
	// Return first error
	for err := range errs {
		if err != nil {
			return err
		}
	}
	
	return nil
}

// PrintStats prints fetch statistics
func (f *Fetcher) PrintStats() {
	fmt.Println("\nFetch Statistics:")
	for url, count := range f.stats {
		fmt.Printf("  %s: %d new items\n", url, count)
	}
}

// fetchFeed fetches and parses a single feed
func fetchFeed(ctx context.Context, url string) ([]FeedItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("User-Agent", "RSS-Reader/1.0")
	req.Header.Set("Accept", "application/rss+xml,application/atom+xml,application/xml")
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	
	// Parse feed
	return parseFeed(resp.Body, url)
}

// parseFeed parses RSS/Atom feed
func parseFeed(r io.Reader, url string) ([]FeedItem, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	
	// Try Atom first, then RSS
	if items := parseAtom(data, url); len(items) > 0 {
		return items, nil
	}
	
	return parseRSS(data, url)
}

// parseAtom parses Atom feed
func parseAtom(data []byte, url string) []FeedItem {
	// Simplified Atom parser
	// In production, use a proper Atom parser
	return []FeedItem{}
}

// parseRSS parses RSS feed
func parseRSS(data []byte, url string) []FeedItem {
	type RSS struct {
		Channel struct {
			Title string `xml:"title"`
			Item  []struct {
				Title   string `xml:"title"`
				Link    string `xml:"link"`
				Desc    string `xml:"description"`
				PubDate string `xml:"pubDate"`
				GUID    string `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	
	var rss RSS
	if err := xml.Unmarshal(data, &rss); err != nil {
		return nil
	}
	
	var items []FeedItem
	for _, item := range rss.Channel.Item {
		pubDate, _ := parseDate(item.PubDate)
		itemID := item.GUID
		if itemID == "" {
			itemID = item.Link
		}
		
		items = append(items, FeedItem{
			Feed:      rss.Channel.Title,
			Title:     cleanText(item.Title),
			Link:      item.Link,
			Published: pubDate,
			Added:     time.Now(),
			ID:        itemID,
		})
	}
	
	return items
}

// Output formats
func outputTable(items []FeedItem, showFeed bool) {
	if len(items) == 0 {
		fmt.Println("No items found")
		return
	}
	
	fmt.Printf("Found %d items:\n\n", len(items))
	
	for i, item := range items {
		date := item.Published.Format("2006-01-02 15:04")
		read := " "
		if item.Read {
			read = "✓"
		}
		star := " "
		if item.Starred {
			star = "★"
		}
		
		if showFeed {
			fmt.Printf("%3d. [%s] %s%s %s\n", i+1, date, read, star, item.Title)
			fmt.Printf("     %s\n", item.Feed)
		} else {
			fmt.Printf("%3d. [%s] %s%s %s\n", i+1, date, read, star, item.Title)
		}
	}
}

func outputJSON(items []FeedItem) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

func outputCSV(items []FeedItem) {
	fmt.Println("feed,title,link,published,read,starred")
	for _, item := range items {
		fmt.Printf("%q,%q,%q,%s,%v,%v\n",
			item.Feed,
			item.Title,
			item.Link,
			item.Published.Format(time.RFC3339),
			item.Read,
			item.Starred,
		)
	}
}

// Helpers
func parseDate(s string) (time.Time, error) {
	formats := []string{
		time.RFC1123,
		time.RFC1123Z,
		time.RFC822,
		time.RFC822Z,
		time.RFC3339,
		time.RFC3339Nano,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"02 Jan 2006 15:04:05 MST",
	}
	
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown format: %s", s)
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	s = html.UnescapeString(s)
	
	// Remove HTML tags
	var result strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				result.WriteRune(r)
			}
		}
	}
	
	return strings.Join(strings.Fields(result.String()), " ")
}

func getDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	
	dir := filepath.Join(home, ".local", "share", "rss-cli")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	
	return dir, nil
}

// loadConfig loads configuration
func loadConfig() (*Config, error) {
	var cfg Config
	
	fs := flag.NewFlagSet("rss", flag.ContinueOnError)
	
	fs.StringSliceVarP(&cfg.Feeds, "feed", "f", []string{}, "Feed URLs (can specify multiple)")
	fs.IntVarP(&cfg.Limit, "limit", "n", 100, "Maximum items to show")
	fs.StringVarP(&cfg.Output, "output", "o", "table", "Output format: table, json, csv")
	fs.DurationVarP(&cfg.Since, "since", "s", 0, "Show items since (e.g., 24h, 7d)")
	fs.IntVarP(&cfg.MaxPerFeed, "max", "m", 100, "Maximum items to store per feed")
	fs.BoolVar(&cfg.NoCache, "no-cache", false, "Disable cache")
	fs.BoolVarP(&cfg.Update, "update", "u", false, "Update feeds")
	fs.BoolVar(&cfg.Purge, "purge", false, "Purge old items")
	fs.StringVar(&cfg.DataDir, "data-dir", "", "Data directory")
	fs.StringVar(&cfg.Format, "format", "", "Custom format string")
	fs.BoolVarP(&cfg.Reverse, "reverse", "r", false, "Reverse order (newest first)")
	
	// Parse flags
	if err := ff.Parse(fs, os.Args[1:],
		ff.WithEnvVarPrefix("RSS"),
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.PlainParser),
	); err != nil {
		return nil, err
	}
	
	// Get data directory
	if cfg.DataDir == "" {
		dir, err := getDataDir()
		if err != nil {
			return nil, err
		}
		cfg.DataDir = dir
	}
	
	// Default feeds if none specified
	if len(cfg.Feeds) == 0 && !cfg.Update {
		cfg.Feeds = []string{
			"https://blog.golang.org/feed.atom",
		}
	}
	
	return &cfg, nil
}

// Main function
func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	
	// Create store
	storePath := filepath.Join(cfg.DataDir, "feeds.json")
	store, err := NewFeedStore(storePath, cfg.MaxPerFeed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create store: %v\n", err)
		os.Exit(1)
	}
	
	// Update feeds if requested
	if cfg.Update || len(cfg.Feeds) > 0 {
		fetcher := NewFetcher(store)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		
		if err := fetcher.FetchAll(ctx, cfg.Feeds); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to fetch feeds: %v\n", err)
		}
		fetcher.PrintStats()
	}
	
	// Calculate since time
	var sinceTime time.Time
	if cfg.Since > 0 {
		sinceTime = time.Now().Add(-cfg.Since)
	}
	
	// List items
	items := store.List(cfg.Limit, "", sinceTime, cfg.Reverse)
	
	// Output
	switch cfg.Output {
	case "json":
		if err := outputJSON(items); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to output JSON: %v\n", err)
		}
	case "csv":
		outputCSV(items)
	default:
		outputTable(items, len(cfg.Feeds) > 1)
	}
}
//optimal batch function
// BatchProcessor processes feeds in batches
type BatchProcessor struct {
	store     *PersistentStore
	fetcher   *Fetcher
	batchSize int
	interval  time.Duration
	done      chan struct{}
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(store *PersistentStore, batchSize int, interval time.Duration) *BatchProcessor {
	return &BatchProcessor{
		store:     store,
		fetcher:   NewFetcher(store),
		batchSize: batchSize,
		interval:  interval,
		done:      make(chan struct{}),
	}
}

// Start starts the batch processor
func (bp *BatchProcessor) Start(ctx context.Context, urls []string) {
	ticker := time.NewTicker(bp.interval)
	defer ticker.Stop()
	
	// Initial fetch
	bp.fetchBatch(ctx, urls)
	
	for {
		select {
		case <-ticker.C:
			bp.fetchBatch(ctx, urls)
		case <-ctx.Done():
			return
		case <-bp.done:
			return
		}
	}
}

// fetchBatch fetches a batch of feeds
func (bp *BatchProcessor) fetchBatch(ctx context.Context, urls []string) {
	// Process in batches
	for i := 0; i < len(urls); i += bp.batchSize {
		end := i + bp.batchSize
		if end > len(urls) {
			end = len(urls)
		}
		
		batch := urls[i:end]
		if err := bp.fetcher.FetchAll(ctx, batch); err != nil {
			// Log error but continue
			continue
		}
		
		// Small delay between batches
		time.Sleep(100 * time.Millisecond)
	}
}

// Stop stops the batch processor
func (bp *BatchProcessor) Stop() {
	close(bp.done)
}
//batch eof

//Persistent Storage Manager 


// PersistentStore manages feed storage with automatic cleanup
type PersistentStore struct {
	mu     sync.RWMutex
	feeds  map[string]*FeedBucket
	path   string
	maxAge time.Duration
}

// FeedBucket stores items for a single feed
type FeedBucket struct {
	Items []FeedItem `json:"items"`
	Meta  FeedMeta   `json:"meta"`
}

// FeedMeta contains feed metadata
type FeedMeta struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Updated   time.Time `json:"updated"`
	Etag      string    `json:"etag,omitempty"`
	LastFetch time.Time `json:"last_fetch"`
}

// NewPersistentStore creates a new store
func NewPersistentStore(path string, maxAge time.Duration) (*PersistentStore, error) {
	s := &PersistentStore{
		feeds:  make(map[string]*FeedBucket),
		path:   path,
		maxAge: maxAge,
	}
	
	if err := s.load(); err != nil {
		return nil, err
	}
	
	return s, nil
}

// AddItems adds items to a feed bucket
func (s *PersistentStore) AddItems(feedURL string, items []FeedItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	bucket, exists := s.feeds[feedURL]
	if !exists {
		bucket = &FeedBucket{
			Meta: FeedMeta{
				URL:       feedURL,
				LastFetch: time.Now(),
			},
		}
		s.feeds[feedURL] = bucket
	}
	
	// Deduplicate
	existing := make(map[string]bool)
	for _, item := range bucket.Items {
		existing[item.ID] = true
	}
	
	// Add new items
	for _, item := range items {
		if existing[item.ID] {
			continue
		}
		bucket.Items = append(bucket.Items, item)
		existing[item.ID] = true
	}
	
	// Sort by date (oldest first)
	sort.SliceStable(bucket.Items, func(i, j int) bool {
		return bucket.Items[i].Published.Before(bucket.Items[j].Published)
	})
	
	// Clean old items
	s.cleanupBucket(bucket)
	
	// Update metadata
	bucket.Meta.LastFetch = time.Now()
	if len(items) > 0 && bucket.Meta.Title == "" {
		bucket.Meta.Title = items[0].Feed
	}
	
	return s.save()
}

// GetItems returns items for a feed
func (s *PersistentStore) GetItems(feedURL string, limit int, since time.Time) []FeedItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	bucket, exists := s.feeds[feedURL]
	if !exists {
		return nil
	}
	
	var result []FeedItem
	for _, item := range bucket.Items {
		if !since.IsZero() && item.Published.Before(since) {
			continue
		}
		result = append(result, item)
	}
	
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	
	return result
}

// GetAllItems returns all items across all feeds
func (s *PersistentStore) GetAllItems(limit int, since time.Time) []FeedItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	var allItems []FeedItem
	for _, bucket := range s.feeds {
		for _, item := range bucket.Items {
			if !since.IsZero() && item.Published.Before(since) {
				continue
			}
			allItems = append(allItems, item)
		}
	}
	
	// Sort by date
	sort.SliceStable(allItems, func(i, j int) bool {
		return allItems[i].Published.Before(allItems[j].Published)
	})
	
	if limit > 0 && len(allItems) > limit {
		allItems = allItems[len(allItems)-limit:]
	}
	
	return allItems
}

// cleanupBucket removes old items
func (s *PersistentStore) cleanupBucket(bucket *FeedBucket) {
	if s.maxAge <= 0 {
		return
	}
	
	cutoff := time.Now().Add(-s.maxAge)
	var filtered []FeedItem
	for _, item := range bucket.Items {
		if item.Published.After(cutoff) {
			filtered = append(filtered, item)
		}
	}
	bucket.Items = filtered
}

// Save saves store to disk
func (s *PersistentStore) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	
	data, err := json.MarshalIndent(s.feeds, "", "  ")
	if err != nil {
		return err
	}
	
	// Atomic write
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	
	return os.Rename(tmpPath, s.path)
}

// load loads store from disk
func (s *PersistentStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return nil
	}
	
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	
	return json.Unmarshal(data, &s.feeds)
}

//Persistent Storage Manager eof go rutine do