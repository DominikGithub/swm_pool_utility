#!/bin/bash

# Build script for SWM Pool Utility
# This script handles the VCS stamping issue

echo "Building SWM Pool Utility..."

# Build with VCS disabled to avoid the error
go build -buildvcs=false -o swm_pool_utility .

if [ $? -eq 0 ]; then
    echo "Build successful!"
    echo ""
    echo "Usage:"
    echo "  ./swm_pool_utility          # Run scraper"
    echo "  ./swm_pool_utility -web     # Start web server"
    echo ""
    echo "Web interface will be available at: http://localhost:8080"
else
    echo "Build failed. Please check Go installation."
    exit 1
fi