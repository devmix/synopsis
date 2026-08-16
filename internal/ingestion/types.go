package ingestion

type Document struct {
	SourcePath string                 // filesystem path to original file
	Content    string                 // raw text content extracted from the file
	Metadata   map[string]interface{} // source_type, file_size, modified_at, etc.
}

type ParseResult struct {
	Documents []Document
	Errors    []error // non-fatal errors encountered during parsing
}

type Parser interface {
	// Parse walks the given source path and returns parsed documents.
	Parse(sourcePath string) ParseResult

	// SupportedExtensions returns the list of file extensions this parser can handle,
	// including the leading dot (e.g. ".md", ".json").
	SupportedExtensions() []string
}
