#!/bin/bash
# Wrapper for zig cc to filter out unsupported flags passed by Go linker (Go 1.23+)
args=()
for arg in "$@"; do
  # Filter out flags that zig cc / zld doesn't support
  if [[ "$arg" == *"-tsaware"* ]] ||      [[ "$arg" == *"-nxcompat"* ]] ||      [[ "$arg" == *"-dynamicbase"* ]] ||      [[ "$arg" == *"-highentropyva"* ]] ||      [[ "$arg" == *"-def:"* ]] ||      [[ "$arg" == *"-T,"* ]]; then
    continue
  fi
  args+=("$arg")
done

zig cc "${args[@]}"
