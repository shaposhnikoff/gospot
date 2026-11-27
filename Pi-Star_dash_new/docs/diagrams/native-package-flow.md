# Native package and request flow

```mermaid
flowchart TD
    Source[Svelte and TypeScript source] --> Vite[Vite build]
    Vite --> Assets[Native UI assets and runtime.json]
    Go[Go source and restricted helpers] --> Package[ARM Debian package]
    Assets --> Package
    Package --> Audit[Reject PHP code, PHP dependencies and classic docroot]
    Audit --> Smoke[Run exact ARM binary in isolation]
    Smoke --> Operator[Go operator service on port 8789]
    Smoke --> Admin[Go HTTPS management service on port 8788]
    Operator --> Static[Serve packaged UI assets]
    Operator --> Status[Read-only allowlisted status and Last Heard API]
    Operator --> Redirect[Redirect supported admin routes to same-board HTTPS]
    Admin --> Auth[Authenticate and check CSRF for writes]
    Auth --> Native[Native management UI and Go API]
    Native --> Helper[Restricted configuration helpers]
```

The reference PHP checkout is not a build input or a runtime dependency. This flow covers native packaging and HTTP serving; it does not claim that all legacy operations have been migrated.

## Isolated interactive preview

```mermaid
flowchart TD
    Browser[Browser on forwarded localhost origin] --> Gateway[Loopback Go preview gateway]
    Gateway --> Assets[Native UI and explicit preview runtime]
    Gateway --> Check[Validate Host and same-origin writes]
    Check --> TLS[Verify fixture TLS certificate and inject ephemeral fixture authentication]
    TLS --> Admin[Isolated Go admin and CSRF validation]
    Admin --> Helper[Shipped helper writes fixture INIs]
    Helper --> Units[Only disposable test systemd units]
    Admin --> Relative[Relative redirects and local preview cookie]
    Relative --> Browser
```

Preview navigation stays on one browser origin. No host port 8788, real board credentials, PHP runtime, or radio hardware is used. The gateway is QA-only; production TLS and authentication remain unchanged.
