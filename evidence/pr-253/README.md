# B13 buyer evidence

- Code head: `f30c887` (`feat/b13-buyer-followups`)
- Matrix: the real `apps/buyer` Vite app, order list and order detail, 360/430px × zh/en/km, light theme.
- Capture: the browser route mocked only the public auth/catalogue and buyer order API responses; it then opened the real order list and clicked the real order card into the real detail view.
- Checks: 12 PNGs captured; the manifest binds each file to width, language, and view; Playwright asserted the detail page had no horizontal overflow at either width.

The API fixture uses a valid parent/sub-order total (USD 25.00) and a public order identifier only. No server address, token, or secret is included in the evidence.
