package config

// Config stores linter settings.
type Config struct {
	Exclude          []string `json:"exclude"`
	SafeNamespaces   []string `json:"safe_namespaces"`
	ScanVendors      []string `json:"scan_vendors"`
	ConsolePath      string   `json:"console_path"`
	Env              string   `json:"env"`
	Verbose          bool     `json:"verbose"`
	BaselinePath     string   `json:"baseline"`
	NoAgent          bool     `json:"-"` // Skip Igor Agent even if available
	ProdPackages     []string `json:"-"` // List of require packages from composer.json
	DevPackages      []string `json:"-"` // List of require-dev packages from composer.json
	GenerateBaseline bool     `json:"-"` // Internal: set if --generate-baseline is used
	OutputFormat     string   `json:"output"`
}

// Baseline represents a collection of ignored findings.
type Baseline struct {
	Files map[string][]BaselineEntry `json:"files"`
}

// BaselineEntry represents a single finding in the baseline.
type BaselineEntry struct {
	Message string `json:"message"`
}
