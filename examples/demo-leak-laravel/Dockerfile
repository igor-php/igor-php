FROM dunglas/frankenphp

# Install dependencies
RUN apt-get update && apt-get install -y \
    libzip-dev \
    unzip \
    && docker-php-ext-install zip pcntl

# Set working directory
WORKDIR /app

# Copy application files
COPY . .

# Install composer dependencies
RUN curl -sS https://getcomposer.org/installer | php -- --install-dir=/usr/local/bin --filename=composer \
    && composer install --no-interaction --optimize-autoloader

# Set permissions
RUN chmod -R 777 storage bootstrap/cache

# Generate key
RUN php artisan key:generate

# Entrypoint will be handled by docker-compose
