---
name: bandwidth-test
description: Measure the node's internet bandwidth with curl against speed.cloudflare.com. Use only when asked to test download or upload speed.
---

# Bandwidth tests

- `curl` against `speed.cloudflare.com` only: one stream first, then 4 in parallel (`xargs -P4`), to tell a per-connection
  limit from the link rate. Download: `curl -s -o /dev/null -w '%{speed_download}\n' 'https://speed.cloudflare.com/__down?bytes=25000000'`.
  Upload: `head -c 10000000 /dev/urandom > <scratchpad>/up.bin`, then
  `curl -s -o /dev/null -w '%{speed_upload}\n' --data-binary @<scratchpad>/up.bin https://speed.cloudflare.com/__up`
  (bytes/s; multiply by 8 for bit/s).
- The home uplink is about 6 Mbit/s and shared with CI: uploads of 10 MB or less, one at a time, and not while an image
  push is running (check the runner pod's tx rate first). Cilium does not rate-limit this pod, so this measures the node's
  path to the internet, not the runner's 300M cap.
