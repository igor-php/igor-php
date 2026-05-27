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
        $proxyClassName = 'IgorUsageProxy_' . str_replace('\\', '_', $className) . '_' . md5($className);
        
        $reflection = new \ReflectionClass($className);
        $realPath = (string) $reflection->getFileName();

        if (!class_exists($proxyClassName)) {
            $methodsCode = '';

            foreach ($reflection->getMethods(\ReflectionMethod::IS_PUBLIC) as $method) {
                // Only skip constructor and destructor, but KEEP __invoke
                if ($method->isConstructor() || $method->isDestructor() || $method->isFinal() || $method->isStatic()) {
                    continue;
                }

                // If it's a magic method other than __invoke, skip for now to avoid side effects
                if (str_starts_with($method->getName(), '__') && $method->getName() !== '__invoke') {
                    continue;
                }

                $params = [];
                foreach ($method->getParameters() as $param) {
                    $paramCode = ($param->hasType() ? (string)$param->getType() . ' ' : '') . '$' . $param->getName();
                    if ($param->isDefaultValueAvailable()) {
                        $paramCode .= ' = ' . var_export($param->getDefaultValue(), true);
                    }
                    $params[] = $paramCode;
                }
                $paramsList = implode(', ', $params);
                $argList = implode(', ', array_map(fn($p) => '$' . $p->getName(), $method->getParameters()));

                $returnType = $method->hasReturnType() ? ': ' . (string)$method->getReturnType() : '';

                $methodsCode .= "
                    public function {$method->getName()}($paramsList)$returnType {
                        \$this->tracker->markAsUsed(\$this->realPath);
                        return \$this->inner->{$method->getName()}($argList);
                    }
                ";
            }

            $code = "
                class $proxyClassName extends $className {
                    private object \$inner;
                    private \$tracker;
                    private string \$realPath;

                    public function __construct(object \$inner, \$tracker, string \$realPath) {
                        \$this->inner = \$inner;
                        \$this->tracker = \$tracker;
                        \$this->realPath = \$realPath;
                        // Mark as used as soon as the proxy is created
                        \$this->tracker->markAsUsed(\$realPath);
                    }

                    public function __call(\$name, \$args) {
                        \$this->tracker->markAsUsed(\$this->realPath);
                        return \$this->inner->\$name(...\$args);
                    }
                    $methodsCode
                }
            ";
            eval($code);
        }

        return new $proxyClassName($inner, $tracker, $realPath);
    }
}
