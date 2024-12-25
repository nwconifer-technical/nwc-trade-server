# NWC Trade API Server

This is the service that runs all of the necessary stuff for nwfinance.

## Deploymnets

The service is deployed on a GCP VM through Cloud Build and runs as a service. It takes ~3 minutes from start to service restart, and as such **SHOULD ONLY BE RUN AT LEAST 15 MINUTES BEFORE AND AT LEAST 5 MINUTES AFTER** the hour. This is to avoid the price logging cron jobs from not running or misrunning.
