#!/bin/sh
set -eu

# Create the Fly.io app.
fly apps create mnp

# Allocate IP addresses for routing.
fly ips allocate-v4 --shared -a mnp
fly ips allocate-v6 -a mnp

# Create a 1GB persistent volume in San Jose (closest region to Seattle).
# This stores the SQLite database and cloned git repo across restarts.
fly volumes create mnp_cache --region sjc --size 1 -a mnp

# 4 shared cores with 1GB RAM. Burst balance from idle time covers
# league night traffic and cold-start syncs. With scale-to-zero this
# costs under $2/month at ~17 hours of usage.
fly scale vm shared-cpu-4x --memory 1024 -a mnp

# Add the custom domain. The output will show IP addresses to configure
# as A/AAAA DNS records in Cloudflare (proxy off, DNS-only).
fly certs add mondaynightplanball.com -a mnp

# Create a deploy token and store it as a GitHub Actions secret so CI
# can deploy on release.
fly tokens create deploy -a mnp | gh secret set FLYIO_TOKEN
