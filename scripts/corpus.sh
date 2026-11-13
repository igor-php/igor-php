#!/usr/bin/env bash
# Regression corpus (#152): runs igor on real open-source applications pinned in
# corpus/projects.json, checks the invariants, and compares the findings per rule with
# corpus/baseline/<project>.json.
#
#   scripts/corpus.sh [--update|--strict] [project...]     (make corpus / make corpus-update)
#
# --update rewrites the baselines after an intended change. --strict (the corpus workflow) fails
# when the findings differ from the baselines, instead of warning. The projects are cloned and installed
# in IGOR_CORPUS_DIR (default: ~/.cache/igor-php/corpus) and kept between runs, so only the first
# run pays for composer install. Each project runs its own code (composer install, cache:clear):
# they are pinned to a commit, and Composer scripts are disabled.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${IGOR_CORPUS_DIR:-$HOME/.cache/igor-php/corpus}
# Absolute: the steps below change directory
mkdir -p "$WORK"
WORK=$(cd "$WORK" && pwd)
UPDATE=0
STRICT=0
ONLY=()
for arg in "$@"; do
    case $arg in
        --update) UPDATE=1 ;;
        --strict) STRICT=1 ;;
        -*) echo "unknown option: $arg" >&2; exit 2 ;;
        *) ONLY+=("$arg") ;;
    esac
done

for tool in git composer php go jq; do
    command -v "$tool" >/dev/null || { echo "❌ $tool is required" >&2; exit 2; }
done

mkdir -p "$WORK/results"
echo "▶ Building igor from $ROOT"
(cd "$ROOT" && go build -o "$WORK/igor" ./cmd/igor)

# selected reports whether the project was asked for (all of them when none is named).
selected() {
    [ ${#ONLY[@]} -eq 0 ] && return 0
    local wanted
    for wanted in "${ONLY[@]}"; do [ "$wanted" = "$1" ] && return 0; done
    return 1
}

# prepare clones the project at its pinned commit, installs it, and installs igor from this checkout.
# It is called as `prepare ... || exit 1`, where Bash ignores set -e: every step returns on failure.
prepare() {
    local project=$1 dir=$2
    local repo commit framework memory
    repo=$(jq -r .repo <<<"$project")
    commit=$(jq -r .commit <<<"$project")
    framework=$(jq -r .framework <<<"$project")
    memory=$(jq -r '.memory // "512M"' <<<"$project")

    if [ ! -d "$dir/.git" ]; then
        git init -q "$dir" && git -C "$dir" remote add origin "$repo" || { echo "  ❌ git init failed" >&2; return 1; }
    fi
    if ! git -C "$dir" cat-file -e "$commit^{commit}" 2>/dev/null; then
        echo "  ↓ fetching $commit"
        git -C "$dir" fetch -q --depth 1 origin "$commit" || { echo "  ❌ cannot fetch $commit" >&2; return 1; }
    fi
    # Restore the project's own files: the steps below change composer.json and the bundles
    git -C "$dir" checkout -q -f "$commit" || { echo "  ❌ cannot check out $commit" >&2; return 1; }

    echo "  ⚙ composer install"
    (cd "$dir" && COMPOSER_MEMORY_LIMIT=-1 composer install --no-interaction --no-progress --no-scripts -q) \
        || { echo "  ❌ composer install failed" >&2; return 1; }

    # igor from this checkout, as a user installs it: composer require --dev igor-php/igor-php
    echo "  ⚙ installing igor from the checkout"
    (cd "$dir" \
        && composer config repositories.igor-local "{\"type\": \"path\", \"url\": \"$ROOT\", \"options\": {\"symlink\": true}}" \
        && composer require --dev --no-update -q "igor-php/igor-php:*@dev" \
        && COMPOSER_MEMORY_LIMIT=-1 composer update -q --no-interaction --no-progress --no-scripts igor-php/igor-php) \
        || { echo "  ❌ installing igor failed" >&2; return 1; }

    local cmd
    while IFS= read -r cmd; do
        [ -n "$cmd" ] || continue
        echo "  ⚙ $cmd"
        (cd "$dir" && bash -c "$cmd") || { echo "  ❌ setup command failed: $cmd" >&2; return 1; }
    done < <(jq -r '(.setup // [])[]' <<<"$project")

    case $framework in
        symfony)
            # As the README says: the bundle in config/bundles.php, then the cache warmup writes the map
            php -r '
                $file = $argv[1];
                $bundles = file_get_contents($file);
                if (!str_contains($bundles, "IgorPhpBundle")) {
                    $bundles = preg_replace("/\];\s*$/", "    IgorPhp\\IgorBundle\\IgorPhpBundle::class => [\"dev\" => true],\n];\n", $bundles);
                    file_put_contents($file, $bundles);
                }' "$dir/config/bundles.php" || { echo "  ❌ cannot register IgorPhpBundle" >&2; return 1; }
            echo "  ⚙ cache:clear (writes the igor service map)"
            (cd "$dir" && rm -rf var/cache && APP_ENV=dev php -d "memory_limit=$memory" bin/console cache:clear --env=dev --no-interaction >"$WORK/results/$(basename "$dir").cache.log" 2>&1) \
                || { echo "  ❌ cache:clear failed, see $WORK/results/$(basename "$dir").cache.log" >&2; return 1; }
            ;;
        laravel)
            # IgorServiceProvider is discovered by Laravel; igor runs php artisan igor:dump itself
            ;;
        *)
            echo "  ❌ unknown framework: $framework" >&2
            return 1
            ;;
    esac
}

status=0
while IFS= read -r project; do
    name=$(jq -r .name <<<"$project")
    selected "$name" || continue
    echo
    echo "▶ $name"
    dir="$WORK/$name"
    rm -f "$WORK/results/$name.status"
    if ! (
        # The project's environment variables (a database driver, a disabled feature...), for its
        # own commands and for igor, which runs php artisan igor:dump on a Laravel project
        while IFS='=' read -r key value; do
            [ -n "$key" ] && export "$key=$value"
        done < <(jq -r '(.env // {}) | to_entries[] | "\(.key)=\(.value)"' <<<"$project")

        prepare "$project" "$dir" || exit 1
        echo "  🔍 igor"
        igor_status=0
        "$WORK/igor" --output json "$dir" >"$WORK/results/$name.json" 2>"$WORK/results/$name.log" || igor_status=$?
        echo "$igor_status" >"$WORK/results/$name.status"
    ); then
        status=1
        continue
    fi
    igor_status=$(cat "$WORK/results/$name.status")

    args=(--name "$name" --framework "$(jq -r .framework <<<"$project")" --commit "$(jq -r .commit <<<"$project")"
        --dir "$dir" --results "$WORK/results/$name.json" --log "$WORK/results/$name.log"
        --igor-status "$igor_status" --baseline "$ROOT/corpus/baseline/$name.json")
    [ "$UPDATE" -eq 1 ] && args+=(--update)
    [ "$STRICT" -eq 1 ] && args+=(--strict)
    php "$ROOT/scripts/corpus-report.php" "${args[@]}" || status=1
done < <(jq -c '.[]' "$ROOT/corpus/projects.json")

exit $status
