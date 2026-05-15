#!/bin/bash
# Wrapper for zig c++ to filter out unsupported flags passed by Go linker (Go 1.23+)
args=()
for arg in "$@"; do
  if [[ "$arg" == *"-tsaware"* ]] ||      [[ "$arg" == *"-nxcompat"* ]] ||      [[ "$arg" == *"-dynamicbase"* ]] ||      [[ "$arg" == *"-highentropyva"* ]] ||      [[ "$arg" == *"-def:"* ]] ||      [[ "$arg" == *"-T,"* ]]; then
    continue
  fi
  args+=("$arg")
done

zig c++ "${args[@]}"
