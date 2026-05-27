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

        if (!class_exists($proxyClassName)) {
            $reflection = new \ReflectionClass($className);
            $methodsCode = '';

            foreach ($reflection->getMethods(\ReflectionMethod::IS_PUBLIC) as $method) {
                if ($method->isConstructor() || $method->isDestructor() || $method->isFinal() || $method->isStatic()) {
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
                        \$this->tracker->markAsUsed(\$this->originalClass);
                        return \$this->inner->{$method->getName()}($argList);
                    }
                ";
            }

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
                    $methodsCode
                }
            ";
            eval($code);
        }

        return new $proxyClassName($inner, $tracker, $className);
    }
}
