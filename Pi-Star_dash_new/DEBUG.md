# Same-origin interactive preview

## Hypotheses and evidence

1. Fixed management port 8788 escapes the forwarded preview origin and reaches an unrelated process. Expected: operator links and redirects contain 8788 while only operator 8790 was launched. Confirmed by source inspection and `ss -ltnp`: 8788 is owned by home-control-gateway-python. Do not stop that process.
2. The initial preview lacked any management backend. Expected: changing only frontend ports cannot make editors usable. Confirmed by the running command and available listeners. Use an isolated real-write fixture behind one loopback QA gateway; preserve the browser origin and production authentication.
3. First Chromium retry reaches Configuration but cannot load fields. Expected: compare the configured helper under the service user and root, and inspect the fixture sudo policy. Confirmed: `protocols-read` is denied under pistar-wizard but works as root; the old standalone install script lacks protocol permissions. Use the shipped Debian sudo policy with only its installation prefix changed in the fixture.

## Experiments

- Initial standalone operator served HTTP 200 but did not prove any management operation. Rejected as insufficient evidence.
- Port-configured Go and TypeScript unit checks passed, but do not address a forwarded browser origin by themselves. Replanned to a single-origin isolated preview gateway.
- `preview-same-origin-chromium.txt`: catalog passed; Configuration never produced a Color Code field. No write occurred. Direct helper comparison confirmed hypothesis 3; corrected the fixture policy source rather than weakening sudo.
- Subsequent evidence is recorded under `qa/evidence/preview-same-origin-*`. None of these experiments use a working Raspberry Pi or host service port 8788.
- Direct-origin Chromium retry passed navigation and persisted fixture writes. The first forwarded `localhost` attempt failed before reaching the application with `ERR_NAME_NOT_RESOLVED`: the test browser deliberately blocked every hostname except the numeric loopback IP. Added an explicit localhost-to-loopback DNS mapping in the browser harness. The forwarded retry then passed both viewport sizes and the same write/read-back/restore operation; product code did not change for this retry.
