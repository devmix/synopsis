package ingestion

import (
	"sync"
	"time"
)

// ProgressStats holds counters for the current ingestion run.
type ProgressStats struct {
	FilesProcessed      int64 `json:"files_processed"`
	ChunksCreated       int64 `json:"chunks_created"`
	EntitiesExtracted   int64 `json:"entities_extracted"`
	EmbeddingsGenerated int64 `json:"embeddings_generated"`
	FactsCreated        int64 `json:"facts_created"`
	FactSourcesCreated  int64 `json:"fact_sources_created"`
	Errors              int64 `json:"errors"`
	DocumentsCreated    int64 `json:"documents_created"`
	DocumentsUpdated    int64 `json:"documents_updated"`
	DocumentsSkipped    int64 `json:"documents_skipped"`
}

// ProgressTracker manages progress reporting during ingestion.
type ProgressTracker struct {
	mu    sync.Mutex
	stats ProgressStats
	//bar     *progressbar.ProgressBar
	started time.Time
	label   string
}

// NewProgressTracker creates a tracker with an optional total file count for the progress bar.
func NewProgressTracker(totalFiles int, label string) *ProgressTracker {
	pt := &ProgressTracker{
		started: time.Now(),
		label:   label,
	}

	//if totalFiles > 0 {
	//	// The progress bar always goes to stderr: stdout is reserved for MCP
	//	// protocol traffic during serve mode.
	//	bar := progressbar.NewOptions64(
	//		int64(totalFiles),
	//		progressbar.OptionSetDescription(label),
	//		progressbar.OptionShowBytes(false),
	//		progressbar.OptionSetWidth(40),
	//		progressbar.OptionThrottle(65*time.Millisecond),
	//		progressbar.OptionSetWriter(os.Stderr),
	//	)
	//	pt.bar = bar
	//}

	return pt
}

// IncrementFiles marks a file as processed.
func (p *ProgressTracker) IncrementFiles() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.FilesProcessed++
	//if p.bar != nil {
	//	_ = p.bar.Add(1) // ignore error from progress bar update
	//}
}

// AddChunks records the number of chunks created for a document.
func (p *ProgressTracker) AddChunks(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.ChunksCreated += n
}

// AddEntities records the number of entities extracted.
func (p *ProgressTracker) AddEntities(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.EntitiesExtracted += n
}

// AddEmbeddings records the number of embeddings generated.
func (p *ProgressTracker) AddEmbeddings(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.EmbeddingsGenerated += n
}

// IncrementErrors records a non-fatal error during processing.
func (p *ProgressTracker) IncrementErrors() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.Errors++
}

// Stats returns a snapshot of the current progress statistics.
func (p *ProgressTracker) Stats() ProgressStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// IncrementDocumentsCreated records a new document created during ingestion.
func (p *ProgressTracker) IncrementDocumentsCreated() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.DocumentsCreated++
}

// IncrementDocumentsUpdated records an existing document updated during ingestion.
func (p *ProgressTracker) IncrementDocumentsUpdated() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.DocumentsUpdated++
}

// IncrementDocumentsSkipped records a document skipped due to unchanged content hash.
func (p *ProgressTracker) IncrementDocumentsSkipped() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.DocumentsSkipped++
}

// AddFacts records the number of facts created during ingestion.
func (p *ProgressTracker) AddFacts(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.FactsCreated += n
}

// AddFactSources records the number of fact sources created during ingestion.
func (p *ProgressTracker) AddFactSources(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.FactSourcesCreated += n
}

// Elapsed returns the time since tracking started.
func (p *ProgressTracker) Elapsed() time.Duration {
	return time.Since(p.started)
}
