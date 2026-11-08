package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	igorphp "github.com/igor-php/igor-php"
	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

const (
	defaultFindingsLimit = 50
	maxFindingsLimit     = 200
	maxExplainedServices = 20
)

const mcpInstructions = `igor audits a PHP project for FrankenPHP worker mode (Symfony, Laravel Octane): shared services keep
their state between requests, so a property written during a request leaks into the next one.
Start with project_info, then audit_project. For each finding, get_rule explains why it leaks and how to fix
it with the framework of the project. After a fix, audit_file checks the file, even before it is saved.
Prefer the framework fix (ResetInterface / kernel.reset on Symfony, scoped() or a RequestReceived listener
on Laravel Octane) over silencing a finding.`

// mcpServer serves igor over MCP for one project. It loads the project once (container dump, Symfony or
// Laravel service map) and audits it as many times as the agent asks.
type mcpServer struct {
	root    string
	cfg     config.Config
	checker *updateChecker

	mu      sync.Mutex
	project *auditProject
}

func newMCPServer(root string, cfg config.Config) *mcpServer {
	// Folding is done by the tools, from their arguments
	cfg.ShowAll = true
	cfg.OutputFormat = "json"
	return &mcpServer{root: root, cfg: cfg, checker: newUpdateChecker()}
}

// handleMCPSubcommand runs `igor mcp [--no-agent] [directory]`: an MCP server on stdin/stdout.
func handleMCPSubcommand(args []string, configPath string) error {
	fs := flag.NewFlagSet(binName+" mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noAgent := fs.Bool("no-agent", false, "Disable the Symfony/Laravel Deep Audit and scan the directory")
	consolePath := fs.String("console", "", "Custom path to Symfony console")
	env := fs.String("env", "", "Symfony environment (default: dev)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s mcp [options] [directory]\n\nRuns igor as an MCP server (stdio) for AI agents.\n\nOptions:\n", binName)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	root := findProjectRoot(abs)

	cfg := config.LoadConfig(root, configPath)
	if *noAgent {
		cfg.NoAgent = true
	}
	if *consolePath != "" {
		cfg.ConsolePath = *consolePath
	}
	if *env != "" {
		cfg.Env = *env
	}

	// stdout carries the protocol only: anything else printed by mistake goes to stderr
	protocolOut := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = protocolOut }()

	fmt.Fprintf(os.Stderr, "🧟 igor %s MCP server for %s\n", Version, root)
	server := newMCPServer(root, cfg).server()
	return server.Run(context.Background(), &mcp.IOTransport{Reader: os.Stdin, Writer: nopWriteCloser{protocolOut}})
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func boolPtr(b bool) *bool { return &b }

// server builds the MCP server: read-only tools, the audit-worker prompt and the rule resources.
func (s *mcpServer) server() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "igor", Title: "igor-php", Version: Version}, &mcp.ServerOptions{Instructions: mcpInstructions})

	readOnly := func(title string) *mcp.ToolAnnotations {
		return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "project_info",
		Description: "Framework of the project (Symfony, Laravel, Octane), whether the Deep Audit of its container runs, " +
			"and the igor version: whether it is the latest release on Packagist and the command to propose to update it. " +
			"Call it first. refresh reloads the container after a change of the service configuration.",
		Annotations: &mcp.ToolAnnotations{Title: "Project info", ReadOnlyHint: true},
	}, s.projectInfo)

	mcp.AddTool(server, &mcp.Tool{
		Name: "audit_project",
		Description: "Audits the whole project for state that leaks between requests in a FrankenPHP worker. " +
			"Findings are sorted by severity and reachability, and paginated (offset, limit). " +
			"Findings in code that never runs during a request (console, profiler, kernel boot) are hidden unless include_out_of_request is set.",
		Annotations: readOnly("Audit project"),
	}, s.auditProjectTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "audit_file",
		Description: "Audits one PHP file of the project with the project context (shared or transient service, resets). " +
			"Pass content to audit an unsaved version of the file, e.g. to check a fix before writing it.",
		Annotations: readOnly("Audit file"),
	}, s.auditFileTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "explain_service",
		Description: "Explains how igor sees the services matching a name (class, service id or file path): shared or not, " +
			"dependencies, verdict and findings.",
		Annotations: readOnly("Explain service"),
	}, s.explainServiceTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "get_rule",
		Description: "Reference of a rule (the rule_id of a finding): why it leaks in worker mode, the Symfony and Laravel fix, " +
			"and its known false positives. Without rule_id, lists the rules.",
		Annotations: readOnly("Get rule"),
	}, s.getRuleTool)

	server.AddPrompt(&mcp.Prompt{
		Name:        "audit-worker",
		Title:       "Audit for FrankenPHP worker mode",
		Description: "Audits the project with igor and fixes the state leaks, the framework way.",
		Arguments: []*mcp.PromptArgument{
			{Name: "path", Description: "Only audit the findings under this path, relative to the project root"},
		},
	}, s.auditWorkerPrompt)

	server.AddResource(&mcp.Resource{
		URI:         "igor://rules",
		Name:        "rules",
		Title:       "igor rules",
		Description: "The list of igor rules.",
		MIMEType:    "text/markdown",
	}, s.readRules)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "igor://rules/{rule_id}",
		Name:        "rule",
		Title:       "igor rule",
		Description: "The reference of one igor rule.",
		MIMEType:    "text/markdown",
	}, s.readRules)

	return server
}

// loaded returns the project, loading it on first use. The caller holds s.mu.
func (s *mcpServer) loaded(refresh bool) (*auditProject, error) {
	if s.project != nil && !refresh {
		return s.project, nil
	}
	project, err := loadAuditProject(s.root, s.cfg)
	if err != nil {
		return nil, err
	}
	s.project = project
	return project, nil
}

// run audits the project, or one file of it when target is set.
func (s *mcpServer) run(target string, content []byte) ([]symbol.AuditStatus, *auditProject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	project, err := s.loaded(false)
	if err != nil {
		return nil, nil, err
	}
	cfg := s.cfg
	cfg.TargetFile = target
	cfg.StdinContent = content
	results, _ := project.audit(&cfg, nil)
	return results, project, nil
}

// resolvePath returns the absolute path of a file of the project. It refuses a path outside the root,
// including through a symbolic link.
func (s *mcpServer) resolvePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.root, abs)
	}
	abs = filepath.Clean(abs)

	root := s.root
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	checked := abs
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		checked = real
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
		// A new file: its directory must be in the project
		checked = filepath.Join(real, filepath.Base(abs))
	}
	if rel, err := filepath.Rel(root, checked); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the project root %s", path, s.root)
	}
	return abs, nil
}

func (s *mcpServer) relative(path string) string {
	if rel, err := filepath.Rel(s.root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// --- project_info

type projectInfoInput struct {
	Refresh bool `json:"refresh,omitempty" jsonschema:"reload the container dump and the Symfony or Laravel service map"`
}

type projectInfoOutput struct {
	Root          string      `json:"root"`
	Framework     string      `json:"framework" jsonschema:"Symfony, Laravel or none"`
	Octane        bool        `json:"octane,omitempty" jsonschema:"laravel/octane is required by the project"`
	DeepAudit     bool        `json:"deep_audit" jsonschema:"the service container of the framework is read: only shared services are audited"`
	Degraded      string      `json:"degraded,omitempty" jsonschema:"why the Deep Audit of the framework fell back to a directory scan"`
	Services      int         `json:"services,omitempty" jsonschema:"number of services in the container"`
	ContainerDump string      `json:"container_dump,omitempty"`
	Baseline      string      `json:"baseline,omitempty"`
	Igor          versionInfo `json:"igor"`
}

func (s *mcpServer) projectInfo(ctx context.Context, _ *mcp.CallToolRequest, in projectInfoInput) (*mcp.CallToolResult, projectInfoOutput, error) {
	s.mu.Lock()
	project, err := s.loaded(in.Refresh)
	s.mu.Unlock()
	if err != nil {
		return nil, projectInfoOutput{}, err
	}

	out := projectInfoOutput{
		Root:          s.root,
		Framework:     frameworkName(project.bridge),
		Degraded:      project.degraded,
		ContainerDump: s.cfg.ContainerDump,
		Baseline:      s.cfg.BaselinePath,
	}
	if b := project.bridge; b != nil {
		out.DeepAudit = true
		if b.Container != nil {
			out.Services = len(b.Container.Definitions)
		}
	}
	out.Octane = requiresPackage(s.root, "laravel/octane")
	out.Igor = checkVersion(ctx, s.checker, s.root)
	return nil, out, nil
}

// requiresPackage tells whether the composer.json of the project requires a package.
func requiresPackage(rootPath, name string) bool {
	data, err := os.ReadFile(filepath.Join(rootPath, "composer.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return false
	}
	_, inRequire := manifest.Require[name]
	_, inDev := manifest.RequireDev[name]
	return inRequire || inDev
}

// --- findings

type mcpFinding struct {
	File         string `json:"file"`
	Line         int    `json:"line"`
	RuleID       string `json:"rule_id"`
	Severity     string `json:"severity" jsonschema:"ERROR or WARNING"`
	Message      string `json:"message"`
	Class        string `json:"class,omitempty"`
	Method       string `json:"method,omitempty"`
	Code         string `json:"code,omitempty"`
	Remediation  string `json:"remediation,omitempty"`
	Reachability string `json:"reachability,omitempty" jsonschema:"HIGH when application code calls it, INFO otherwise"`
	OutOfRequest string `json:"out_of_request,omitempty" jsonschema:"why this code never runs during a request (console, profiler, kernel boot...)"`
}

func (s *mcpServer) toFinding(file string, f symbol.Finding) mcpFinding {
	return mcpFinding{
		File:         s.relative(file),
		Line:         f.Line,
		RuleID:       f.Rule,
		Severity:     f.Severity,
		Message:      f.Message,
		Class:        f.ContextClass,
		Method:       f.ContextMethod,
		Code:         strings.TrimSpace(f.Code),
		Remediation:  f.Remediation,
		Reachability: f.Reachability,
		OutOfRequest: f.OutOfRequest,
	}
}

func sortFindings(findings []mcpFinding) {
	rank := func(f mcpFinding) int {
		r := 0
		if f.Severity != "ERROR" {
			r += 2
		}
		if f.Reachability != "HIGH" {
			r++
		}
		return r
	}
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}

// --- audit_project

type auditProjectInput struct {
	Severity            string `json:"severity,omitempty" jsonschema:"only return findings of this severity: ERROR or WARNING"`
	RuleID              string `json:"rule_id,omitempty" jsonschema:"only return findings of this rule"`
	Path                string `json:"path,omitempty" jsonschema:"only return findings of files under this path, relative to the project root"`
	IncludeOutOfRequest bool   `json:"include_out_of_request,omitempty" jsonschema:"also return findings in code that never runs during a request"`
	Offset              int    `json:"offset,omitempty" jsonschema:"index of the first finding to return"`
	Limit               int    `json:"limit,omitempty" jsonschema:"maximum number of findings to return (default 50, at most 200)"`
}

type auditProjectOutput struct {
	FilesAudited       int          `json:"files_audited"`
	Total              int          `json:"total" jsonschema:"findings matching the filters"`
	Errors             int          `json:"errors"`
	Warnings           int          `json:"warnings"`
	HiddenOutOfRequest int          `json:"hidden_out_of_request,omitempty" jsonschema:"findings hidden because their code never runs during a request"`
	Findings           []mcpFinding `json:"findings"`
	NextOffset         int          `json:"next_offset,omitempty" jsonschema:"offset of the next page; absent on the last page"`
	Degraded           string       `json:"degraded,omitempty"`
}

// findingFilter keeps the findings audit_project returns.
type findingFilter struct {
	severity string
	ruleID   string
	prefix   string
}

func (s *mcpServer) newFindingFilter(in auditProjectInput) (findingFilter, error) {
	f := findingFilter{severity: strings.ToUpper(strings.TrimSpace(in.Severity)), ruleID: in.RuleID}
	if f.severity != "" && f.severity != "ERROR" && f.severity != "WARNING" {
		return f, fmt.Errorf("severity must be ERROR or WARNING, got %q", in.Severity)
	}
	if f.ruleID != "" {
		if _, ok := igorphp.RuleDoc(f.ruleID); !ok {
			return f, fmt.Errorf("unknown rule %q, expected one of: %s", f.ruleID, strings.Join(igorphp.RuleIDs(), ", "))
		}
	}
	if in.Path != "" {
		abs, err := s.resolvePath(in.Path)
		if err != nil {
			return f, err
		}
		if rel := s.relative(abs); rel != "." {
			f.prefix = strings.TrimSuffix(rel, "/")
		}
	}
	return f, nil
}

func (f findingFilter) matches(finding mcpFinding) bool {
	if f.severity != "" && finding.Severity != f.severity {
		return false
	}
	if f.ruleID != "" && !strings.EqualFold(finding.RuleID, f.ruleID) {
		return false
	}
	return f.prefix == "" || finding.File == f.prefix || strings.HasPrefix(finding.File, f.prefix+"/")
}

func (s *mcpServer) auditProjectTool(_ context.Context, _ *mcp.CallToolRequest, in auditProjectInput) (*mcp.CallToolResult, auditProjectOutput, error) {
	filter, err := s.newFindingFilter(in)
	if err != nil {
		return nil, auditProjectOutput{}, err
	}
	results, project, err := s.run("", nil)
	if err != nil {
		return nil, auditProjectOutput{}, err
	}

	out := auditProjectOutput{FilesAudited: len(results), Degraded: project.degraded, Findings: []mcpFinding{}}
	var all []mcpFinding
	for _, res := range results {
		for _, f := range res.Findings {
			finding := s.toFinding(res.FilePath, f)
			switch {
			case !filter.matches(finding):
			case finding.OutOfRequest != "" && !in.IncludeOutOfRequest:
				out.HiddenOutOfRequest++
			case finding.Severity == "ERROR":
				out.Errors++
				all = append(all, finding)
			default:
				out.Warnings++
				all = append(all, finding)
			}
		}
	}
	sortFindings(all)
	out.Total = len(all)
	out.Findings, out.NextOffset = paginate(all, in.Offset, in.Limit)
	return nil, out, nil
}

// paginate returns the page of findings starting at offset, and the offset of the next page (0 on the last).
func paginate(all []mcpFinding, offset, limit int) ([]mcpFinding, int) {
	if limit <= 0 {
		limit = defaultFindingsLimit
	}
	limit = min(limit, maxFindingsLimit)
	offset = max(offset, 0)
	if offset >= len(all) {
		return []mcpFinding{}, 0
	}
	end := min(offset+limit, len(all))
	if end < len(all) {
		return all[offset:end], end
	}
	return all[offset:end], 0
}

// --- audit_file

type auditFileInput struct {
	Path    string  `json:"path" jsonschema:"the PHP file, absolute or relative to the project root"`
	Content *string `json:"content,omitempty" jsonschema:"unsaved content of the file to audit instead of the file on disk"`
}

type auditFileOutput struct {
	File     string       `json:"file"`
	Audited  bool         `json:"audited" jsonschema:"false when igor skips the file: excluded, transient service, or not a shared service"`
	Shared   bool         `json:"shared,omitempty"`
	Findings []mcpFinding `json:"findings"`
}

func (s *mcpServer) auditFileTool(_ context.Context, _ *mcp.CallToolRequest, in auditFileInput) (*mcp.CallToolResult, auditFileOutput, error) {
	abs, err := s.resolvePath(in.Path)
	if err != nil {
		return nil, auditFileOutput{}, err
	}
	var content []byte
	if in.Content != nil {
		content = []byte(*in.Content)
	} else if _, err := os.Stat(abs); err != nil {
		return nil, auditFileOutput{}, fmt.Errorf("cannot read %s: %w", in.Path, err)
	}

	results, _, err := s.run(abs, content)
	if err != nil {
		return nil, auditFileOutput{}, err
	}
	out := auditFileOutput{File: s.relative(abs), Findings: []mcpFinding{}}
	for _, res := range results {
		out.Audited = true
		out.Shared = out.Shared || res.IsShared
		for _, f := range res.Findings {
			out.Findings = append(out.Findings, s.toFinding(res.FilePath, f))
		}
	}
	sortFindings(out.Findings)
	return nil, out, nil
}

// --- explain_service

type explainServiceInput struct {
	Name string `json:"name" jsonschema:"class name, service id or file path, or a part of it (case-insensitive)"`
}

type explainedService struct {
	ServiceID    string       `json:"service_id,omitempty"`
	File         string       `json:"file"`
	Shared       bool         `json:"shared"`
	Public       bool         `json:"public,omitempty"`
	Verdict      string       `json:"verdict" jsonschema:"OK when igor finds no state leak, KO otherwise"`
	Dependencies []string     `json:"dependencies,omitempty"`
	OutOfRequest string       `json:"out_of_request,omitempty"`
	Findings     []mcpFinding `json:"findings"`
}

type explainServiceOutput struct {
	Framework string             `json:"framework"`
	Matches   int                `json:"matches"`
	Services  []explainedService `json:"services"`
}

func (s *mcpServer) explainServiceTool(_ context.Context, _ *mcp.CallToolRequest, in explainServiceInput) (*mcp.CallToolResult, explainServiceOutput, error) {
	name := strings.ToLower(strings.TrimSpace(in.Name))
	if name == "" {
		return nil, explainServiceOutput{}, errors.New("name is required")
	}
	results, project, err := s.run("", nil)
	if err != nil {
		return nil, explainServiceOutput{}, err
	}
	out := explainServiceOutput{Framework: frameworkName(project.bridge), Services: []explainedService{}}
	sort.SliceStable(results, func(i, j int) bool { return results[i].FilePath < results[j].FilePath })
	for _, res := range results {
		file := s.relative(res.FilePath)
		if !strings.Contains(strings.ToLower(res.ServiceID), name) && !strings.Contains(strings.ToLower(file), name) && !findingsMention(res.Findings, name) {
			continue
		}
		out.Matches++
		if len(out.Services) >= maxExplainedServices {
			continue
		}
		svc := explainedService{
			ServiceID:    res.ServiceID,
			File:         file,
			Shared:       res.IsShared,
			Public:       res.IsPublic,
			Verdict:      "OK",
			Dependencies: res.Dependencies,
			OutOfRequest: res.OutOfRequest,
			Findings:     []mcpFinding{},
		}
		for _, f := range res.Findings {
			svc.Findings = append(svc.Findings, s.toFinding(res.FilePath, f))
		}
		if len(svc.Findings) > 0 {
			svc.Verdict = "KO"
		}
		sortFindings(svc.Findings)
		out.Services = append(out.Services, svc)
	}
	return nil, out, nil
}

func findingsMention(findings []symbol.Finding, name string) bool {
	for _, f := range findings {
		if strings.Contains(strings.ToLower(f.ContextClass), name) {
			return true
		}
	}
	return false
}

// --- get_rule

type getRuleInput struct {
	RuleID string `json:"rule_id,omitempty" jsonschema:"the rule_id of a finding; omit it to list the rules"`
}

type getRuleOutput struct {
	RuleID string   `json:"rule_id,omitempty"`
	Doc    string   `json:"doc,omitempty" jsonschema:"the reference of the rule, in Markdown"`
	Rules  []string `json:"rules,omitempty"`
}

func (s *mcpServer) getRuleTool(_ context.Context, _ *mcp.CallToolRequest, in getRuleInput) (*mcp.CallToolResult, getRuleOutput, error) {
	if strings.TrimSpace(in.RuleID) == "" {
		return nil, getRuleOutput{Rules: igorphp.RuleIDs()}, nil
	}
	doc, ok := igorphp.RuleDoc(in.RuleID)
	if !ok {
		return nil, getRuleOutput{}, fmt.Errorf("unknown rule %q, expected one of: %s", in.RuleID, strings.Join(igorphp.RuleIDs(), ", "))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "# " + in.RuleID + "\n\n" + doc}}}, getRuleOutput{RuleID: in.RuleID, Doc: doc}, nil
}

// --- resources

func (s *mcpServer) readRules(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	id, isRule := strings.CutPrefix(uri, "igor://rules/")
	text := ""
	switch {
	case uri == "igor://rules":
		var b strings.Builder
		b.WriteString("# igor rules\n\n")
		for _, rule := range igorphp.RuleIDs() {
			fmt.Fprintf(&b, "- [%s](igor://rules/%s)\n", rule, rule)
		}
		text = b.String()
	case isRule:
		doc, ok := igorphp.RuleDoc(id)
		if !ok {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		text = "# " + id + "\n\n" + doc
	default:
		return nil, mcp.ResourceNotFoundError(uri)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: text}}}, nil
}

// --- prompt

func (s *mcpServer) auditWorkerPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	scope := "the project"
	pathArg := ""
	if p := strings.TrimSpace(req.Params.Arguments["path"]); p != "" {
		scope = "the files under " + p
		pathArg = fmt.Sprintf(" with path %q", p)
	}
	text := fmt.Sprintf(`Prepare %s for FrankenPHP worker mode with igor.

1. Call project_info. Tell me the framework, whether the Deep Audit runs (and why not if degraded), and if igor is
   not up to date, propose its update_command to me: do not run it without my approval.
2. Call audit_project%s. Start with the ERROR findings of HIGH reachability.
3. For each rule_id you meet, call get_rule once and follow its fix for the framework of the project
   (Symfony: ResetInterface / kernel.reset, Laravel Octane: scoped(), a RequestReceived listener or warm/flush).
   Check its false positives before changing code.
4. Fix one service at a time, then call audit_file on it to check the finding is gone. Run the tests of the project.
5. Do not silence a finding to get a clean audit. If you think a finding is a false positive, explain why and leave
   it to me.
6. Finish with a summary: what you fixed, what is left and why.`, scope, pathArg)
	return &mcp.GetPromptResult{
		Description: "Audit and fix state leaks for FrankenPHP worker mode",
		Messages:    []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text}}},
	}, nil
}

func frameworkName(b *auditor.SymfonyBridge) string {
	if b == nil {
		return "none"
	}
	return b.Framework
}
