# PR #251 evidence

Evidence branch: `evidence/pr-251` bound to merchant code head `c38acd3` (`#251 fix: keep merchant order identifiers readable`).

The matrix contains the real `SuborderListView`, `SuborderDetailView`, and shared `Tabs` component at 360px and 430px in zh, en, and km. The temporary evidence entry uses the merchant Vite app, checked-in UI/merchant styles, and typed mock `MerchantSuborder` data. The list opens the real detail view through the rendered order button; the detail capture verifies the full English order identifier, SKU, quantity, unit price, and line total.

Validation: `pnpm exec tsc -b && pnpm exec vite build`, then `pnpm exec vite preview --host 127.0.0.1 --port 5174 --strictPort`, followed by the Playwright matrix capture. Each capture asserts no horizontal overflow and the detail capture asserts the unit price is visible. No deployment, merge, server connection, or owner approval was performed.
