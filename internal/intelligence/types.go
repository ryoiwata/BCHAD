package intelligence

import "time"

// StageType identifies the pipeline stage a code pattern belongs to.
type StageType string

const (
	StageTypeMigrate StageType = "migrate"
	StageTypeAPI     StageType = "api"
	StageTypeTests   StageType = "tests"
	StageTypeConfig  StageType = "config"
	StageTypeFrontend StageType = "frontend"
)

// ConfigFile represents a configuration file extracted from a repo.
type ConfigFile struct {
	Name    string `json:"name"`
	RelPath string `json:"rel_path"`
	Content string `json:"content,omitempty"`
}

// FileCount tallies source files by classification.
type FileCount struct {
	Source    int `json:"source"`
	Test      int `json:"test"`
	Migration int `json:"migration"`
	Config    int `json:"config"`
	Other     int `json:"other"`
}

// StructuralProfile is the top-level object stored in S3 for a product.
// It captures directory layout, framework detection, and config file contents.
type StructuralProfile struct {
	ProductID       string            `json:"product_id"`
	RepoPath        string            `json:"repo_path"`
	Language        string            `json:"language"`       // "typescript", "go", "python"
	Framework       string            `json:"framework"`      // "express", "chi", "fastapi"
	ORM             string            `json:"orm"`            // "prisma", "pgx", "sqlalchemy"
	TestFramework   string            `json:"test_framework"` // "jest", "go_testing", "pytest"
	DirectoryLayout map[string]string `json:"directory_layout"`
	ConfigFiles     []ConfigFile      `json:"config_files"`
	PrismaSchema    string            `json:"prisma_schema,omitempty"`
	PackageJSON     string            `json:"package_json,omitempty"`
	FileCount       FileCount         `json:"file_count"`
	ScannedAt       time.Time         `json:"scanned_at"`
}

// PatternMetadata holds per-file scoring signals and extracted structural elements.
type PatternMetadata struct {
	FilePath          string    `json:"file_path"`
	RelativePath      string    `json:"relative_path"`
	GitDate           time.Time `json:"git_date"`
	RecencyScore      float64   `json:"recency_score"`
	CompletenessScore float64   `json:"completeness_score"`
	ReviewQuality     float64   `json:"review_quality"`
	Elements          []string  `json:"elements"` // structural elements found by Tree-sitter
}

// CodePattern is a single extracted CRUD-shaped example ready for embedding.
// One CodePattern becomes one row in bchad_code_patterns.
type CodePattern struct {
	// Database fields (populated after upsert)
	ID string `json:"id,omitempty"`

	// Identity
	ProductID  string    `json:"product_id"`
	StageType  StageType `json:"stage_type"`
	Language   string    `json:"language"`
	EntityType string    `json:"entity_type,omitempty"` // e.g. "Article", "User"

	// Feature flags
	HasPermissions  bool     `json:"has_permissions"`
	HasAudit        bool     `json:"has_audit"`
	HasIntegrations []string `json:"has_integrations"` // e.g. ["vault", "launchdarkly"]

	// Quality scoring (composite: recency*0.3 + completeness*0.4 + review*0.3)
	QualityScore float64 `json:"quality_score"`

	// Content stored for retrieval
	ContentText string          `json:"content_text"`
	Metadata    PatternMetadata `json:"metadata"`

	// Embedding (populated by indexer)
	Embedding []float32 `json:"embedding,omitempty"`

	LastUpdated time.Time `json:"last_updated"`
}

// IndexResult summarises a completed indexing run.
type IndexResult struct {
	ProductID           string                      `json:"product_id"`
	FilesScanned        int                         `json:"files_scanned"`
	PatternsPerStage    map[StageType]int           `json:"patterns_per_stage"`
	EmbeddingsStored    int                         `json:"embeddings_stored"`
	ProfileS3Key        string                      `json:"profile_s3_key"`
	PatternsByStage     map[StageType][]CodePattern `json:"patterns_by_stage,omitempty"`
	Error               string                      `json:"error,omitempty"`
}
