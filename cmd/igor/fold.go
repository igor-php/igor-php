package main

import (
	"path/filepath"
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

// outOfRequestFiles maps each file to the reason the services of every class it declares never run
// during a request. A file is left out as soon as one of its classes backs a service that may run
// during a request: a file is audited as a whole, so its findings are only folded when none of them
// can come from request code.
func outOfRequestFiles(aud *auditor.Auditor) map[string]string {
	return outOfRequestBy(aud, func(def symbol.SymfonyService) (string, bool) {
		path, found := aud.Symfony.ClassToFile[def.Class]
		return filepath.Clean(path), found
	})
}

// outOfRequestBy groups the audited services by key and returns the reason each group never runs
// during a request. When the services of a group have several reasons, the first in alphabetical
// order is kept, so that the result does not depend on the map iteration order.
func outOfRequestBy(aud *auditor.Auditor, key func(symbol.SymfonyService) (string, bool)) map[string]string {
	categories := make(map[string]string)
	inRequest := make(map[string]bool)
	for id, def := range aud.Symfony.Container.Definitions {
		if skip, _ := shouldSkipServiceMeta(id, def, aud); skip {
			continue
		}
		k, ok := key(def)
		if !ok {
			continue
		}
		category := outOfRequestCategory(id, def)
		if category == "" {
			inRequest[k] = true
			continue
		}
		if current, found := categories[k]; !found || category < current {
			categories[k] = category
		}
	}
	for k := range inRequest {
		delete(categories, k)
	}
	return categories
}

const (
	symfonyKernel           = `Symfony\Component\HttpKernel\Kernel`
	symfonyMicroKernelTrait = `Symfony\Bundle\FrameworkBundle\Kernel\MicroKernelTrait`
)

// markOutOfRequest flags the findings of code that never runs during a request: the files
// classified at collection time, and the container building methods of the Symfony kernel.
// extends reports whether a class inherits from another; it is nil outside Symfony.
func markOutOfRequest(results []symbol.AuditStatus, extends func(class, ancestor string) bool) {
	for i := range results {
		for j := range results[i].Findings {
			f := &results[i].Findings[j]
			switch {
			case results[i].OutOfRequest != "":
				f.OutOfRequest = results[i].OutOfRequest
			case extends != nil && isKernelBootFinding(*f, extends):
				f.OutOfRequest = kernelBootCategory
			}
		}
	}
}

// isKernelBootFinding matches the Symfony kernel by its full name: the HttpKernel Kernel and the
// application kernels extending it, and the MicroKernelTrait. Another class named Kernel is not one.
func isKernelBootFinding(f symbol.Finding, extends func(class, ancestor string) bool) bool {
	// A grouped finding is only folded when every method that mutates the property builds the container
	if !kernelBootMethods[f.ContextMethod] {
		return false
	}
	for _, m := range f.AlsoMethods {
		if !kernelBootMethods[m] {
			return false
		}
	}
	class := strings.TrimPrefix(f.ContextClass, `\`)
	return strings.EqualFold(class, symfonyMicroKernelTrait) || extends(class, symfonyKernel)
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
