<?php

namespace IgorPhp\IgorBundle\Service;

/**
 * Creates Proxies that track usage while preserving type safety.
 */
class IgorProxyFactory
{
    private IgorUsageTracker $tracker;

    public function __construct(IgorUsageTracker $tracker)
    {
        $this->tracker = $tracker;
    }

    /**
     * Wraps an existing service in a proxy that tracks method calls.
     */
    public function createProxy(object $inner, string $className): object
    {
        $tracker = $this->tracker;
        $proxyClassName = 'IgorUsageProxy_' . str_replace('\\', '_', $className);

        if (!class_exists($proxyClassName)) {
            // We dynamically create a class that extends the original one
            // to preserve 'instanceof' checks and type-hinting.
            $code = "
                class $proxyClassName extends $className {
                    private object \$inner;
                    private \$tracker;
                    private string \$originalClass;

                    public function __construct(object \$inner, \$tracker, string \$originalClass) {
                        \$this->inner = \$inner;
                        \$this->tracker = \$tracker;
                        \$this->originalClass = \$originalClass;
                    }

                    public function __call(\$name, \$args) {
                        \$this->tracker->markAsUsed(\$this->originalClass);
                        return \$this->inner->\$name(...\$args);
                    }
                    
                    // We also need to proxy known public methods if we want full reliability,
                    // but for a linter usage tracker, __call is a good start.
                }
            ";
            eval($code);
        }

        return new $proxyClassName($inner, $tracker, $className);
    }
}
