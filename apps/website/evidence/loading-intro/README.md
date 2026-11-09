# Loading intro evidence

Validated against the local Astro production preview on 2026-11-09.

## Performance

Lighthouse 13.0.1, default mobile profile, three runs per revision:

| Revision | Runs | Median |
| --- | --- | --- |
| Before | 57, 62, 56 | 57 |
| After | 59, 63, 57 | 59 |

The median changed by +2 points, with no regression against the requested 3-point limit.

## Browser checks

- The intro is absent when the page finishes within 300 ms.
- A deliberately delayed page shows the intro after 300 ms and removes it within 1.2 seconds of appearing.
- A second visit in the same session is suppressed.
- `prefers-reduced-motion: reduce` suppresses the intro.
- With JavaScript disabled, the hero remains visible and no overlay is rendered.
- Chinese, English and Khmer home pages were captured at 390 px and 1440 px; both header and footer expose the shared `DR.LIFE+` wordmark with `aria-label="DR.LIFE+"`.

## Files

- Animation frames: `loading-01-appear.png`, `loading-02-progress.png`, `loading-03-fade.png`
- Chinese: `home-zh-390.jpg`, `home-zh-1440.jpg`
- English: `home-en-390.jpg`, `home-en-1440.jpg`
- Khmer: `home-km-390.jpg`, `home-km-1440.jpg`

The delayed resource used to capture the animation exists only in the browser test; it is not part of the website build.
