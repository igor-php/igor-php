package main

import (
	"sort"
	"strings"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// outOfRequestTags are the Symfony tags of the services that never run while the worker serves a request:
// commands run in the console, warmers and clearers at deploy time, loaders when the router or translator
// cache is built, and data collectors only in the profiler of the dev container.
var outOfRequestTags = []struct{ tag, category string }{
	{"console.command", "console"},
	{"data_collector", "profiler"},
	{"kernel.cache_warmer", "cache warmer"},
	{"kernel.cache_clearer", "cache clearer"},
	{"routing.loader", "routing loader"},
	{"translation.loader", "translation loader"},
}

// kernelBootCategory marks the Kernel methods that build or load the container once per worker.
const kernelBootCategory = "kernel boot"

// kernelBootMethods only run when the kernel builds or loads its container. boot() is not one of them:
// a booted kernel resets the services from it on every request. Neither are handle(), reboot() and
// shutdown(), which run in the worker.
var kernelBootMethods = map[string]bool{
	"__construct":                    true,
	"initializeBundles":              true,
	"initializeContainer":            true,
	"preBoot":                        true,
	"build":                          true,
	"buildContainer":                 true,
	"prepareContainer":               true,
	"getContainerBuilder":            true,
	"getContainerLoader":             true,
	"getContainerClass":              true,
	"getContainerBaseClass":          true,
	"getKernelParameters":            true,
	"dumpContainer":                  true,
	"registerBundles":                true,
	"registerContainerConfiguration": true,
	"configureContainer":             true,
	"configureRoutes":                true,
	"loadRoutes":                     true,
}

// outOfRequestCategory returns why a service never runs during a request, or "" when it may.
func outOfRequestCategory(id string, def symbol.SymfonyService) string {
	for _, t := range outOfRequestTags {
		if def.HasTag(t.tag) {
			return t.category
		}
	}
	// Debug decorators (debug.event_dispatcher, TraceableAdapter...) only exist in the dev container
	if def.HasTag("container.decorator") && (strings.HasPrefix(id, "debug.") || strings.HasPrefix(shortClassName(def.Class), "Traceable")) {
		return "debug"
	}
	return ""
}

// outOfRequestClasses maps each class to the reason its services never run during a request.
// A class is left out as soon as one of its audited services may run during a request.
func outOfRequestClasses(aud *auditor.Auditor) map[string]string {
	categories := make(map[string]string)
	inRequest := make(map[string]bool)
	for id, def := range aud.Symfony.Container.Definitions {
		if skip, _ := shouldSkipServiceMeta(id, def, aud); skip {
			continue
		}
		category := outOfRequestCategory(id, def)
		if category == "" {
			inRequest[def.Class] = true
			continue
		}
		if _, found := categories[def.Class]; !found {
			categories[def.Class] = category
		}
	}
	for class := range inRequest {
		delete(categories, class)
	}
	return categories
}

// markOutOfRequest flags the findings of code that never runs during a request: the services
// classified at collection time, and the container building methods of the Symfony kernel.
func markOutOfRequest(results []symbol.AuditStatus, symfony bool) {
	for i := range results {
		for j := range results[i].Findings {
			f := &results[i].Findings[j]
			switch {
			case results[i].OutOfRequest != "":
				f.OutOfRequest = results[i].OutOfRequest
			case symfony && isKernelBootFinding(*f):
				f.OutOfRequest = kernelBootCategory
			}
		}
	}
}

func isKernelBootFinding(f symbol.Finding) bool {
	class := shortClassName(f.ContextClass)
	return (class == "Kernel" || class == "MicroKernelTrait") && kernelBootMethods[f.ContextMethod]
}

// foldOutOfRequest recomputes each file status without its out-of-request findings,
// so that the summary and the exit code only count the code that runs during a request.
func foldOutOfRequest(results []symbol.AuditStatus) {
	for i := range results {
		folded := false
		for _, f := range results[i].Findings {
			if f.OutOfRequest != "" {
				folded = true
				break
			}
		}
		if folded {
			results[i].Status = calculateAuditStatus(inRequestFindings(results[i].Findings))
		}
	}
}

func inRequestFindings(findings []symbol.Finding) []symbol.Finding {
	var kept []symbol.Finding
	for _, f := range findings {
		if f.OutOfRequest == "" {
			kept = append(kept, f)
		}
	}
	return kept
}

// foldedSummary counts the active out-of-request findings and lists their categories.
func foldedSummary(results []symbol.AuditStatus) (int, []string) {
	count := 0
	seen := make(map[string]bool)
	for _, res := range results {
		for _, f := range res.Findings {
			if f.OutOfRequest == "" || f.Ignored {
				continue
			}
			count++
			seen[f.OutOfRequest] = true
		}
	}
	categories := make([]string, 0, len(seen))
	for c := range seen {
		categories = append(categories, c)
	}
	sort.Strings(categories)
	return count, categories
}

func shortClassName(class string) string {
	return class[strings.LastIndex(class, `\`)+1:]
}

// foldsOutOfRequest tells whether out-of-request findings are folded: only in the CLI output,
// since the JSON and LLM exports keep every finding with its out_of_request reason.
func foldsOutOfRequest(cfg config.Config) bool {
	return !cfg.ShowAll && cfg.OutputFormat != "json" && cfg.OutputFormat != "llm"
}
