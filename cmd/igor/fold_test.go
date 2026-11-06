package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func tagged(names ...string) []any {
	tags := make([]any, 0, len(names))
	for _, n := range names {
		tags = append(tags, map[string]any{"name": n})
	}
	return tags
}

func TestOutOfRequestCategory(t *testing.T) {
	tests := []struct {
		name string
		id   string
		def  symbol.SymfonyService
		want string
	}{
		{"console command", "console.command.cache_clear", symbol.SymfonyService{Class: `X\CacheClearCommand`, Tags: tagged("console.command")}, "console"},
		{"data collector", "data_collector.request", symbol.SymfonyService{Class: `X\RequestDataCollector`, Tags: tagged("data_collector", "kernel.reset")}, "profiler"},
		{"cache warmer", "x.warmer", symbol.SymfonyService{Class: `X\Warmer`, Tags: tagged("kernel.cache_warmer")}, "cache warmer"},
		{"cache clearer", "x.clearer", symbol.SymfonyService{Class: `X\Clearer`, Tags: tagged("kernel.cache_clearer")}, "cache clearer"},
		{"routing loader", "routing.loader.yml", symbol.SymfonyService{Class: `X\YamlFileLoader`, Tags: tagged("routing.loader")}, "routing loader"},
		{"translation loader", "translation.loader.xliff", symbol.SymfonyService{Class: `X\XliffFileLoader`, Tags: tagged("translation.loader")}, "translation loader"},
		{"debug decorator by id", "debug.event_dispatcher", symbol.SymfonyService{Class: `X\EventDispatcher`, Tags: tagged("container.decorator")}, "debug"},
		{"Traceable decorator", "cache.app", symbol.SymfonyService{Class: `Symfony\Component\Cache\Adapter\TraceableAdapter`, Tags: tagged("container.decorator")}, "debug"},
		// A decorator in production code runs during the request it decorates
		{"other decorator", "app.cached_repository", symbol.SymfonyService{Class: `App\CachedRepository`, Tags: tagged("container.decorator")}, ""},
		// Without the decorator tag, the debug. prefix alone proves nothing
		{"debug id without decoration", "debug.file_link_formatter", symbol.SymfonyService{Class: `X\FileLinkFormatter`}, ""},
		{"untagged service", "router", symbol.SymfonyService{Class: `X\Router`, Tags: []any{}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outOfRequestCategory(tt.id, tt.def); got != tt.want {
				t.Errorf("outOfRequestCategory(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestOutOfRequestFiles_KeepsFilesThatAlsoRunDuringARequest(t *testing.T) {
	cfg := config.Config{NoAgent: true}
	aud := auditor.NewAuditor(cfg)
	bridge := auditor.NewSymfonyBridge(t.TempDir(), "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
		"app.command":        {Class: `App\ReportCommand`, Shared: true, Tags: tagged("console.command")},
		"app.shared_command": {Class: `App\SharedCommand`, Shared: true, Tags: tagged("console.command")},
		// The same class also backs a service that runs during a request
		"app.shared_service": {Class: `App\SharedCommand`, Shared: true},
		// A skipped (non-shared) definition must not keep its class in the request
		"app.prototype": {Class: `App\ReportCommand`, Shared: false},
		// A file declaring a command and a request service is not folded
		"app.mixed_command": {Class: `App\MixedCommand`, Shared: true, Tags: tagged("console.command")},
		"app.mixed_service": {Class: `App\MixedService`, Shared: true},
		// Several reasons for one file: the choice does not depend on the map order
		"app.warmer":  {Class: `App\Warmer`, Shared: true, Tags: tagged("kernel.cache_warmer")},
		"app.clearer": {Class: `App\Clearer`, Shared: true, Tags: tagged("kernel.cache_clearer")},
	}}
	bridge.ClassToFile = map[string]string{
		`App\ReportCommand`: "/app/src/ReportCommand.php",
		`App\SharedCommand`: "/app/src/SharedCommand.php",
		`App\MixedCommand`:  "/app/src/Mixed.php",
		`App\MixedService`:  "/app/src/Mixed.php",
		`App\Warmer`:        "/app/src/Cache.php",
		`App\Clearer`:       "/app/src/Cache.php",
	}
	aud.Symfony = bridge

	got := outOfRequestFiles(aud)
	// The paths are cleaned, which uses the OS separator
	want := map[string]string{
		filepath.Clean("/app/src/ReportCommand.php"): "console",
		filepath.Clean("/app/src/Cache.php"):         "cache clearer",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outOfRequestFiles() = %v, want %v", got, want)
	}
}

func TestCollectFiles_MarksOutOfRequestServices(t *testing.T) {
	cfg := config.Config{NoAgent: true}
	aud := auditor.NewAuditor(cfg)
	root := t.TempDir()
	bridge := auditor.NewSymfonyBridge(root, "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
		"app.command": {Class: `App\ReportCommand`, Shared: true, Tags: tagged("console.command")},
		"app.service": {Class: `App\Service`, Shared: true, Tags: tagged("kernel.reset")},
	}}
	bridge.ClassToFile = map[string]string{
		`App\ReportCommand`: root + "/src/ReportCommand.php",
		`App\Service`:       root + "/src/Service.php",
	}
	aud.Symfony = bridge

	got := make(map[string]string)
	for _, item := range collectFiles(root, cfg, aud) {
		got[item.ServiceID] = item.OutOfRequest
	}
	want := map[string]string{"app.command": "console", "app.service": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collected out-of-request reasons = %v, want %v", got, want)
	}
}

func TestMarkOutOfRequest_KernelBootMethodsOnly(t *testing.T) {
	kernel := `Symfony\Component\HttpKernel\Kernel`
	extends := func(class, ancestor string) bool {
		return class == ancestor || (class == `App\Kernel` && ancestor == kernel)
	}
	results := []symbol.AuditStatus{
		{ServiceID: "app.command", OutOfRequest: "console", Findings: []symbol.Finding{{ContextClass: `App\ReportCommand`, ContextMethod: "execute"}}},
		{ServiceID: "Inherited/" + kernel, Findings: []symbol.Finding{
			{ContextClass: kernel, ContextMethod: "initializeContainer"},
			{ContextClass: `Symfony\Bundle\FrameworkBundle\Kernel\MicroKernelTrait`, ContextMethod: "registerContainerConfiguration"},
			// These run in the worker: boot() resets the services of a booted kernel on every request
			{ContextClass: kernel, ContextMethod: "boot"},
			{ContextClass: kernel, ContextMethod: "handle"},
			{ContextClass: kernel, ContextMethod: "reboot"},
			{ContextClass: kernel, ContextMethod: "shutdown"},
			// A method of the same name in another class is not the kernel boot
			{ContextClass: `App\Container`, ContextMethod: "initializeContainer"},
			// The application kernel extends the Symfony one
			{ContextClass: `App\Kernel`, ContextMethod: "configureContainer"},
			// Another class named Kernel is not the Symfony kernel
			{ContextClass: `App\Rendering\Kernel`, ContextMethod: "build"},
			// A property also written in handle() changes during requests
			{ContextClass: kernel, ContextMethod: "build", AlsoMethods: []string{"handle"}},
			{ContextClass: kernel, ContextMethod: "build", AlsoMethods: []string{"configureContainer"}},
		}},
	}
	markOutOfRequest(results, extends)

	var got []string
	for _, res := range results {
		for _, f := range res.Findings {
			got = append(got, f.ContextMethod+"="+f.OutOfRequest)
		}
	}
	want := []string{
		"execute=console",
		"initializeContainer=kernel boot",
		"registerContainerConfiguration=kernel boot",
		"boot=", "handle=", "reboot=", "shutdown=",
		"initializeContainer=",
		"configureContainer=kernel boot",
		"build=",
		"build=", "build=kernel boot",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("marked findings = %v, want %v", got, want)
	}

	// The kernel rule only applies to Symfony
	laravel := []symbol.AuditStatus{{Findings: []symbol.Finding{{ContextClass: kernel, ContextMethod: "initializeContainer"}}}}
	markOutOfRequest(laravel, nil)
	if laravel[0].Findings[0].OutOfRequest != "" {
		t.Errorf("kernel boot rule applied outside Symfony")
	}
}

func TestFoldOutOfRequest_StatusOnlyCountsRequestCode(t *testing.T) {
	results := []symbol.AuditStatus{
		{Status: "❌ KO", Findings: []symbol.Finding{{Severity: "ERROR", OutOfRequest: "console"}}},
		{Status: "❌ KO", Findings: []symbol.Finding{{Severity: "ERROR", OutOfRequest: "kernel boot"}, {Severity: "WARNING"}}},
		{Status: "❌ KO", Findings: []symbol.Finding{{Severity: "ERROR"}}},
		{Status: "❌ ERROR"},
	}
	foldOutOfRequest(results)

	var got []string
	for _, res := range results {
		got = append(got, res.Status)
	}
	want := []string{"✅ OK", "⚠️  WARN", "❌ KO", "❌ ERROR"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statuses = %q, want %q", got, want)
	}
}

func TestFoldsOutOfRequest(t *testing.T) {
	tests := []struct {
		output  string
		showAll bool
		want    bool
	}{
		{"cli", false, true},
		{"", false, true},
		{"cli", true, false},
		{"json", false, false},
		{"llm", false, false},
	}
	for _, tt := range tests {
		if got := foldsOutOfRequest(config.Config{OutputFormat: tt.output, ShowAll: tt.showAll}); got != tt.want {
			t.Errorf("foldsOutOfRequest(output=%q, showAll=%v) = %v, want %v", tt.output, tt.showAll, got, tt.want)
		}
	}
}

type foldingReporter struct {
	mockReporter
	printed          []symbol.Finding
	foldedCount      int
	foldedCategories []string
}

func (r *foldingReporter) PrintFindings(res symbol.AuditStatus, _ string, _ bool) {
	r.printed = append(r.printed, res.Findings...)
}

func (r *foldingReporter) PrintFolded(count int, categories []string) {
	r.foldedCount = count
	r.foldedCategories = categories
}

func TestReportAllFindings_FoldsOutOfRequestFindings(t *testing.T) {
	results := []symbol.AuditStatus{
		{FilePath: "/app/vendor/x/Command.php", Findings: []symbol.Finding{{Message: "command", OutOfRequest: "console"}}},
		{FilePath: "/app/vendor/x/Kernel.php", Findings: []symbol.Finding{
			{Message: "boot", OutOfRequest: "kernel boot"},
			{Message: "handle"},
			// Baseline-ignored findings are not counted as folded
			{Message: "ignored", OutOfRequest: "kernel boot", Ignored: true},
		}},
		{FilePath: "/app/src/Service.php", Findings: []symbol.Finding{{Message: "service"}}},
	}

	rep := &foldingReporter{}
	reportAllFindings(rep, results, "/app", true)
	var printed []string
	for _, f := range rep.printed {
		printed = append(printed, f.Message)
	}
	if want := []string{"service", "handle"}; !reflect.DeepEqual(printed, want) {
		t.Errorf("printed findings = %v, want %v", printed, want)
	}
	if rep.foldedCount != 2 || !reflect.DeepEqual(rep.foldedCategories, []string{"console", "kernel boot"}) {
		t.Errorf("folded notice = %d %v, want 2 [console kernel boot]", rep.foldedCount, rep.foldedCategories)
	}
	// Folding must not change the results themselves (baseline, JSON)
	if len(results[1].Findings) != 3 {
		t.Errorf("reportAllFindings modified the results: %+v", results[1].Findings)
	}

	all := &foldingReporter{}
	reportAllFindings(all, results, "/app", false)
	if len(all.printed) != 5 || all.foldedCount != 0 {
		t.Errorf("--show-all printed %d findings and a folded notice of %d, want 5 and none", len(all.printed), all.foldedCount)
	}
}
