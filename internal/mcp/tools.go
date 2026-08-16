package mcp

import (
	"context"
	"database/sql"

	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	// ToolSearch performs combined lexical and semantic search.
	ToolSearch = "search"

	// ToolCatalogOverview returns aggregate statistics about the knowledge base.
	ToolCatalogOverview = "catalog_overview"

	// ToolCatalogDocuments lists documents with cursor pagination.
	ToolCatalogDocuments = "catalog_documents"

	// ToolCatalogEntities lists entities with cursor pagination.
	ToolCatalogEntities = "catalog_entities"

	// ToolSearchEntitiesByType searches entities by type with pagination.
	ToolSearchEntitiesByType = "search_entities_by_type"

	// ToolSearchFacts searches facts with optional filters and pagination.
	ToolSearchFacts = "search_facts"

	// ToolGetDocumentContext retrieves document metadata, chunks, entities, and fact IDs.
	ToolGetDocumentContext = "get_document_context"

	// ToolGetChunkByID retrieves a single chunk by Predicate with document info and entities.
	ToolGetChunkByID = "get_chunk_by_id"

	// ToolGetFactByID retrieves a single fact by Predicate with entities and sources.
	ToolGetFactByID = "get_fact_by_id"

	// ToolGetEntityDossier retrieves full entity dossier with facts, sources, related entities, and cross-domain links.
	ToolGetEntityDossier = "get_entity_dossier"

	// ToolGetEntityRelations traverses the knowledge graph from an entity Predicate or name.
	ToolGetEntityRelations = "get_entity_relations"

	// ToolGetEntityLinks retrieves cross-domain entity links with provenance by entity Predicate or name.
	ToolGetEntityLinks = "get_entity_links"
)

// registerTools adds all MCP tools to the server with their handlers.
func (s *Server) registerTools() {
	s.server.AddTool(toolSearch(), s.handleSearch)
	s.server.AddTool(toolCatalogOverview(), s.handleCatalogOverview)
	s.server.AddTool(toolCatalogDocuments(), s.handleCatalogDocuments)
	s.server.AddTool(toolCatalogEntities(), s.handleCatalogEntities)
	s.server.AddTool(toolSearchEntitiesByType(), s.handleSearchEntitiesByType)
	s.server.AddTool(toolSearchFacts(), s.handleSearchFacts)
	s.server.AddTool(toolGetDocumentContext(), s.handleGetDocumentContext)
	s.server.AddTool(toolGetChunkByID(), s.handleGetChunkByID)
	s.server.AddTool(toolGetFactByID(), s.handleGetFactByID)
	s.server.AddTool(toolGetEntityDossier(), s.handleGetEntityDossier)
	s.server.AddTool(toolGetEntityRelations(), s.handleGetEntityRelations)
	s.server.AddTool(toolGetEntityLinks(), s.handleGetEntityLinks)
}

// toolSearch defines the search MCP tool schema.
func toolSearch() mcp.Tool {
	return mcp.NewTool(
		ToolSearch,
		mcp.WithDescription("Performs hybrid search over the knowledge base combining lexical (FTS5/BM25) and semantic (vector/cosine) results via Reciprocal Rank Fusion. Returns ranked text chunks with full metadata including document_id, chunk_id, sequence_num, offsets, domains, score, and associated entities."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search query string"),
		),
		mcp.WithNumber("top_k",
			mcp.Description("Maximum number of results to return (default 10, range 1-100)"),
			mcp.DefaultNumber(10),
		),
		mcp.WithString("domain",
			mcp.Description("Filter results by domain (e.g., 'hr', 'product', 'engineering'). Empty or omitted = search all domains."),
		),
	)
}

// toolCatalogOverview defines the catalog_overview MCP tool schema.
func toolCatalogOverview() mcp.Tool {
	return mcp.NewTool(
		ToolCatalogOverview,
		mcp.WithDescription("Returns aggregate statistics about the knowledge base including document count, chunk count, entity count, fact count, documents by type, entities by type and domain, list of domains, entity types, and graph node/edge counts."),
	)
}

// toolCatalogDocuments defines the catalog_documents MCP tool schema.
func toolCatalogDocuments() mcp.Tool {
	return mcp.NewTool(
		ToolCatalogDocuments,
		mcp.WithDescription("Lists documents in the knowledge base with cursor-based pagination. Supports filtering by domain, source type, and name (substring match on original_path). Returns document metadata including id, source_type, original_path, domain array, parsed metadata, created_at, and updated_at."),
		mcp.WithNumber("page_size",
			mcp.Description("Number of items per page (1-200, default 20)"),
			mcp.DefaultNumber(20),
		),
		mcp.WithString("cursor",
			mcp.Description("Base64-encoded cursor for pagination. Omit or empty to start from the beginning."),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Empty = all domains."),
		),
		mcp.WithString("source_type",
			mcp.Description("Filter by source type (e.g., 'json', 'markdown'). Empty = all types."),
		),
		mcp.WithString("name",
			mcp.Description("Filter documents by name substring match on original_path (case-insensitive)."),
		),
	)
}

// toolCatalogEntities defines the catalog_entities MCP tool schema.
func toolCatalogEntities() mcp.Tool {
	return mcp.NewTool(
		ToolCatalogEntities,
		mcp.WithDescription("Lists entities in the knowledge base with cursor-based pagination. Supports filtering by entity type, domain, and name (substring match on entity name). Returns entity metadata including id, name, type, domain, description, confidence, and parsed metadata."),
		mcp.WithNumber("page_size",
			mcp.Description("Number of items per page (1-200, default 20)"),
			mcp.DefaultNumber(20),
		),
		mcp.WithString("cursor",
			mcp.Description("Base64-encoded cursor for pagination. Omit or empty to start from the beginning."),
		),
		mcp.WithString("type",
			mcp.Description("Filter by entity type (e.g., 'employee', 'department'). Empty = all types."),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Empty = all domains."),
		),
		mcp.WithString("name",
			mcp.Description("Filter entities by name substring match (case-insensitive)."),
		),
	)
}

// toolSearchEntitiesByType defines the search_entities_by_type MCP tool schema.
func toolSearchEntitiesByType() mcp.Tool {
	return mcp.NewTool(
		ToolSearchEntitiesByType,
		mcp.WithDescription("Searches entities by type with cursor-based pagination. Returns entity details including id, name, type, domain, description, confidence, and metadata."),
		mcp.WithString("entity_type",
			mcp.Required(),
			mcp.Description("Entity type to filter by (e.g., 'employee', 'department', 'policy')"),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Empty = all domains."),
		),
		mcp.WithNumber("page_size",
			mcp.Description("Number of items per page (1-200, default 20)"),
			mcp.DefaultNumber(20),
		),
		mcp.WithString("cursor",
			mcp.Description("Base64-encoded cursor for pagination. Omit or empty to start from the beginning."),
		),
	)
}

// toolGetDocumentContext defines the get_document_context MCP tool schema.
func toolGetDocumentContext() mcp.Tool {
	return mcp.NewTool(
		ToolGetDocumentContext,
		mcp.WithDescription("Retrieves full document context including metadata, all chunks with offsets and sequence numbers, associated entities, and fact IDs. Supports selective inclusion of chunks, entities, and facts."),
		mcp.WithString("document_id",
			mcp.Required(),
			mcp.Description("Document Predicate in the database (integer as string)"),
		),
		mcp.WithBoolean("include_chunks",
			mcp.Description("Include chunk data with offsets and text (default true)"),
			mcp.DefaultBool(true),
		),
		mcp.WithBoolean("include_entities",
			mcp.Description("Include entities associated with document chunks (default true)"),
			mcp.DefaultBool(true),
		),
		mcp.WithBoolean("include_facts",
			mcp.Description("Include fact IDs from approved facts linked to entity in this document (default false)"),
			mcp.DefaultBool(false),
		),
	)
}

// toolGetChunkByID defines the get_chunk_by_id MCP tool schema.
func toolGetChunkByID() mcp.Tool {
	return mcp.NewTool(
		ToolGetChunkByID,
		mcp.WithDescription("Retrieves a single chunk by Predicate with full text, offsets, document metadata, and associated entities."),
		mcp.WithString("chunk_id",
			mcp.Required(),
			mcp.Description("Chunk Predicate in the database (integer as string)"),
		),
	)
}

// toolGetFactByID defines the get_fact_by_id MCP tool schema.
func toolGetFactByID() mcp.Tool {
	return mcp.NewTool(
		ToolGetFactByID,
		mcp.WithDescription("Retrieves a single fact by Predicate with subject/object entity details and source document quotes."),
		mcp.WithString("fact_id",
			mcp.Required(),
			mcp.Description("Fact Predicate in the database (integer as string)"),
		),
	)
}

// toolGetEntityDossier defines the get_entity_dossier MCP tool schema.
func toolGetEntityDossier() mcp.Tool {
	return mcp.NewTool(
		ToolGetEntityDossier,
		mcp.WithDescription("Retrieves a complete entity dossier including all approved facts, source documents, related entities via graph traversal (BFS), and cross-domain links with provenance. Accepts either entity_id or entity_name (exactly one required). Only approved facts are included in expansion."),
		mcp.WithString("entity_id",
			mcp.Description("Entity Predicate in the database (integer as string). Provide this OR entity_name, not both."),
		),
		mcp.WithString("entity_name",
			mcp.Description("Entity name for lookup. Case-insensitive exact match. Provide this OR entity_id, not both. Use type/domain to disambiguate if multiple entities share the same name."),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Used with entity_name to disambiguate when multiple entities share the same name. Case-insensitive."),
		),
		mcp.WithNumber("depth",
			mcp.Description("BFS traversal depth for related entities, range 1-5 (default 2)"),
			mcp.DefaultNumber(2),
		),
		mcp.WithBoolean("include_facts",
			mcp.Description("Include approved facts linked to this entity (default true)"),
		),
		mcp.WithBoolean("include_sources",
			mcp.Description("Include source documents for this entity (default true)"),
		),
	)
}

// toolGetEntityRelations defines the get_entity_relations MCP tool schema.
func toolGetEntityRelations() mcp.Tool {
	return mcp.NewTool(
		ToolGetEntityRelations,
		mcp.WithDescription("Traverses the knowledge graph from a given entity using BFS. Accepts either entity_id or entity_name (exactly one required). Returns connected nodes and edges up to the specified depth. Use include_cross_domain=true to follow cross-domain entity links."),
		mcp.WithString("entity_id",
			mcp.Description("Entity Predicate in the database (integer as string). Provide this OR entity_name, not both."),
		),
		mcp.WithString("entity_name",
			mcp.Description("Entity name for lookup. Case-insensitive exact match. Provide this OR entity_id, not both. Use type/domain to disambiguate if multiple entities share the same name."),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Used with entity_name to disambiguate when multiple entities share the same name. Case-insensitive."),
		),
		mcp.WithNumber("depth",
			mcp.Description("BFS traversal depth, range 1-10 (default 2)"),
			mcp.DefaultNumber(2),
		),
		mcp.WithBoolean("include_cross_domain",
			mcp.Description("Follow cross-domain entity links during traversal (default false)"),
		),
	)
}

// toolGetEntityLinks defines the get_entity_links MCP tool schema.
func toolGetEntityLinks() mcp.Tool {
	return mcp.NewTool(
		ToolGetEntityLinks,
		mcp.WithDescription("Retrieves cross-domain entity links for a given entity with full provenance information including method (rule/equals/llm), confidence, and evidence. Accepts either entity_id or entity_name (exactly one required). Links are created automatically during ingestion."),
		mcp.WithString("entity_id",
			mcp.Description("Entity Predicate in the database (integer as string). Provide this OR entity_name, not both."),
		),
		mcp.WithString("entity_name",
			mcp.Description("Entity name for lookup. Case-insensitive exact match. Provide this OR entity_id, not both. Use type/domain to disambiguate if multiple entities share the same name."),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Used with entity_name to disambiguate when multiple entities share the same name. Case-insensitive."),
		),
	)
}

// handleSearch delegates to the search handler.
func (s *Server) handleSearch(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// TODO init newDBDomainValidator as struct member
	return handlers.HandleSearch(ctx, req, s.searcher, newDBDomainValidator(s.db.DB()))
}

// dbDomainValidator validates domain names against the knowledge base.
type dbDomainValidator struct {
	db *sql.DB
}

func newDBDomainValidator(db *sql.DB) *dbDomainValidator {
	return &dbDomainValidator{db: db}
}

func (v *dbDomainValidator) IsKnownDomain(domain string) bool {
	if v.db == nil || domain == "" {
		return true // no DB or empty domain → skip validation
	}
	const query = `SELECT 1 FROM documents, json_each(metadata_json, '$.domain') WHERE metadata_json IS NOT NULL AND json_valid(metadata_json) AND json_each.value = ? LIMIT 1`
	var _dummy int
	return v.db.QueryRowContext(context.Background(), query, domain).Scan(&_dummy) == nil
}

// handleCatalogOverview delegates to the catalog overview handler.
func (s *Server) handleCatalogOverview(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleCatalogOverview(ctx, req, s.db.DB())
}

// handleCatalogDocuments delegates to the catalog documents handler.
func (s *Server) handleCatalogDocuments(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleCatalogDocuments(ctx, req, s.db.DB())
}

// handleCatalogEntities delegates to the catalog entities handler.
func (s *Server) handleCatalogEntities(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleCatalogEntities(ctx, req, s.db.DB())
}

// handleSearchEntitiesByType delegates to the search entities by type handler.
func (s *Server) handleSearchEntitiesByType(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleSearchEntitiesByType(ctx, req, s.db.DB())
}

// toolSearchFacts defines the search_facts MCP tool schema.
func toolSearchFacts() mcp.Tool {
	return mcp.NewTool(
		ToolSearchFacts,
		mcp.WithDescription("Searches facts with optional filters including predicate (LIKE), entity_name (subject or object name match), status (default 'approved'), and domain. Returns fact details with entity names and cursor-based pagination."),
		mcp.WithString("predicate",
			mcp.Description("Filter by predicate substring match (case-insensitive)."),
		),
		mcp.WithString("entity_name",
			mcp.Description("Filter facts where subject OR object entity name matches this substring (case-insensitive)."),
		),
		mcp.WithString("status",
			mcp.Description("Filter by fact status. Default: 'approved'. Use 'pending' or other statuses as needed."),
		),
		mcp.WithString("domain",
			mcp.Description("Filter by domain (e.g., 'hr', 'product'). Empty = all domains."),
		),
		mcp.WithNumber("page_size",
			mcp.Description("Number of items per page (1-200, default 20)"),
			mcp.DefaultNumber(20),
		),
		mcp.WithString("cursor",
			mcp.Description("Base64-encoded cursor for pagination. Omit or empty to start from the beginning."),
		),
	)
}

// handleSearchFacts delegates to the search facts handler.
func (s *Server) handleSearchFacts(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleSearchFacts(ctx, req, s.db.DB())
}

// handleGetDocumentContext delegates to the document context handler.
func (s *Server) handleGetDocumentContext(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleGetDocumentContext(ctx, req, s.db.DB())
}

// handleGetChunkByID delegates to the chunk by Predicate handler.
func (s *Server) handleGetChunkByID(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleGetChunkByID(ctx, req, s.db.DB())
}

// handleGetFactByID delegates to the fact by Predicate handler.
func (s *Server) handleGetFactByID(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleGetFactByID(ctx, req, s.db.DB())
}

// handleGetEntityDossier delegates to the entity dossier handler.
func (s *Server) handleGetEntityDossier(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleGetEntityDossier(ctx, req, s.db.DB(), s.graph.Load())
}

// handleGetEntityRelations delegates to the entity relations handler.
func (s *Server) handleGetEntityRelations(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if s.graph.Load() == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Knowledge graph is not available (disabled or not loaded yet)"),
			},
			IsError: true,
		}, nil
	}
	return handlers.HandleGetEntityRelations(ctx, req, s.db.DB(), s.graph.Load())
}

// handleGetEntityLinks delegates to the entity links handler.
func (s *Server) handleGetEntityLinks(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return handlers.HandleGetEntityLinks(ctx, req, s.db.DB())
}
