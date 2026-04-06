#!/bin/bash
# Migration 002: NO-OP — db-cleanup service (deliberately removed)
#
# This migration was originally created for a dedicated db-cleanup service that
# ran periodic VACUUM operations.  The service was removed before it was ever
# deployed: VACUUM is now handled inside the weather-forecast-scraper (weekly,
# Sundays at 03:00 UTC) and PRAGMA auto_vacuum = INCREMENTAL is set at DB init.
#
# The script is kept as a placeholder so that the migration numbering stays
# contiguous and production deployments can apply 001 → 002 → 003 → … in order
# without confusion.
#
# Safe to run at any time — it does nothing.

echo "Migration 002: no-op (db-cleanup service was removed before deployment). Nothing to do."
