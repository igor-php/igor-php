FROM dunglas/frankenphp

# Composer, so dependencies can be installed without PHP/Composer on the host
COPY --from=composer:2 /usr/bin/composer /usr/bin/composer

# zip extension + unzip/git let Composer download and extract packages, pcntl is required by Laravel Octane
RUN apt-get update \
    && apt-get install -y --no-install-recommends git unzip \
    && rm -rf /var/lib/apt/lists/* \
    && install-php-extensions zip pcntl

ENV COMPOSER_ALLOW_SUPERUSER=1
