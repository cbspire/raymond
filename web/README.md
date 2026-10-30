# Raymond dashboard

React 19 + TypeScript, Tailwind CSS 4, shadcn/ui component patterns (Radix Dialog/Slot, CVA) and Lucide icons. Components live in `src/components`, behavior and lifecycle cleanup in `src/hooks`, and protocol types/client in `src/lib`. `components.json` configures the shadcn registry aliases for future components. Raymond's paper/burgundy palette and compact layout are preserved with shared theme tokens.

Use Node 22.12 or newer:

```sh
npm ci --prefix web
npm run check --prefix web
npm run build --prefix web
```

Commit source, `package-lock.json` and the generated `internal/proxy/web` assets together. CI rebuilds from the lockfile and rejects stale generated assets. Ordinary Go builds use those checked-in assets and do not require npm, Node or network access. The installed executable remains self-contained; React, Radix, Tailwind and icons are bundled locally, with no CDN requests.

`npm run dev --prefix web` serves the frontend on loopback. Authentication/API requests require the real enrolled manager; it does not mock privileged actions. The controlled browser fixture is documented in `test/browser/dashboard.cjs` and serves the production build for UI tests/screenshots. These tests do not establish native lifecycle acceptance.

Dialogs use a fresh server-generated style nonce for Radix scroll locking. The bootstrap reads only that nonce from a meta tag; scripts/styles remain self-hosted, inline scripts are forbidden, and arbitrary inline styles are not permitted by CSP. The browser test records CSP violations across navigation and tests focus/Escape behavior.

Themes follow the OS until explicitly selected and persist in local storage. Launch credentials stay in a removed URL fragment, API credentials use same-origin cookies/CSRF headers, config/logs render as React text, and drafts preserve their original saved revision after a failed save.
