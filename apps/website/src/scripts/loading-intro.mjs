const SESSION_KEY = "drlife-loading-intro-seen";
const SHOW_DELAY_MS = 300;
const MAX_VISIBLE_MS = 1200;
const FADE_MS = 180;

export function initLoadingIntro(windowObject, documentObject) {
  if (windowObject.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

  try {
    if (windowObject.sessionStorage.getItem(SESSION_KEY)) return;
  } catch {
    // Storage can be unavailable in privacy modes; loading feedback still works.
  }

  const template = documentObject.getElementById("site-loading-template");
  if (!template?.content.firstElementChild) return;

  let overlay;
  const remove = () => overlay?.remove();
  const hide = () => {
    if (!overlay || overlay.classList.contains("is-leaving")) return;
    overlay.classList.add("is-leaving");
    windowObject.setTimeout(remove, FADE_MS);
  };
  const showTimer = windowObject.setTimeout(() => {
    if (documentObject.readyState === "complete") return;
    overlay = template.content.firstElementChild.cloneNode(true);
    documentObject.body.append(overlay);
    try {
      windowObject.sessionStorage.setItem(SESSION_KEY, "1");
    } catch {
      // The overlay must never block the page when storage is unavailable.
    }
    windowObject.requestAnimationFrame(() => overlay?.classList.add("is-visible"));
    windowObject.setTimeout(hide, MAX_VISIBLE_MS - FADE_MS);
  }, SHOW_DELAY_MS);

  windowObject.addEventListener("load", () => {
    windowObject.clearTimeout(showTimer);
    hide();
  }, { once: true });
}
