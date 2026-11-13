package auditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// A getter declared to return an interface or an abstract class can return a shared service: the
// type is shared when the container aliases it to a shared service, or when a shared service
// implements or extends it. Only a concrete class without a shared definition, or a type no shared
// service has, names a per-call object.
func TestAbstractTypesMayBeSharedServices(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"Types.php": `<?php
namespace App\Factory;
interface FactoryInterface {}
interface PersistentInterface extends FactoryInterface {}
abstract class Factory implements PersistentInterface {}
abstract class ObjectFactory extends Factory {}
final class PostFactory extends ObjectFactory {}
interface HandlerInterface {}
final class Handler implements HandlerInterface {}
abstract class AbstractString {}
final class UnicodeString extends AbstractString {}
final class Temporary {}
final class Prototype {}`,
	}
	paths := map[string]string{}
	for name, code := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	classToFile := map[string]string{}
	for _, class := range []string{"FactoryInterface", "PersistentInterface", "Factory", "ObjectFactory", "PostFactory",
		"HandlerInterface", "Handler", "AbstractString", "UnicodeString", "Temporary", "Prototype"} {
		classToFile[`App\Factory\`+class] = paths["Types.php"]
	}

	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{
		Container: &symbol.SymfonyContainer{
			Definitions: map[string]symbol.SymfonyService{
				`App\Factory\PostFactory`: {Class: `App\Factory\PostFactory`, Shared: true},
				`app.handler`:             {Class: `App\Factory\Handler`, Shared: true},
				`App\Factory\Prototype`:   {Class: `App\Factory\Prototype`, Shared: false},
			},
			Aliases: map[string]interface{}{
				`App\Factory\HandlerInterface`: "app.handler",
				// An internal interface: no source file to read
				`Iterator`: map[string]interface{}{"id": "app.handler"},
			},
		},
		ClassToFile: classToFile,
	}

	for class, want := range map[string]bool{
		`App\Factory\PostFactory`:         true,  // a shared definition
		`App\Factory\ObjectFactory`:       true,  // abstract, extended by PostFactory
		`App\Factory\Factory`:             true,  // abstract, extended through ObjectFactory
		`App\Factory\PersistentInterface`: true,  // implemented by Factory
		`\App\Factory\FactoryInterface`:   true,  // extended by PersistentInterface
		`App\Factory\HandlerInterface`:    true,  // aliased to app.handler
		`\Iterator`:                       true,  // aliased, without a source file
		`App\Factory\AbstractString`:      false, // no shared service extends it
		`App\Factory\Temporary`:           false, // concrete, no definition
		`App\Factory\Prototype`:           false, // concrete, not shared
	} {
		if got := aud.IsSharedService(class); got != want {
			t.Errorf("IsSharedService(%s) = %v, want %v", class, got, want)
		}
	}
}
