const FONT_WAIT_MS = 600;
const MAX_VISIBLE_MS = 1200;
const FADE_MS = 180;

export function initLoadingIntro(windowObject, documentObject) {
  const overlay = documentObject.getElementById("site-loading-intro");
  if (!overlay) return;

  let removed = false;
  let hardTimer;
  const remove = () => {
    if (removed) return;
    removed = true;
    windowObject.clearTimeout(hardTimer);
    overlay.remove();
    documentObject.documentElement.classList.remove("intro");
  };
  if (!documentObject.documentElement.classList.contains("intro")) {
    remove();
    return;
  }

  const hide = () => {
    if (removed || overlay.classList.contains("is-leaving")) return;
    overlay.classList.add("is-leaving");
    const remaining = Math.max(0, MAX_VISIBLE_MS - windowObject.performance.now());
    windowObject.setTimeout(remove, Math.min(FADE_MS, remaining));
  };

  const finishAfterFonts = async () => {
    const fontsReady = documentObject.fonts?.ready ?? Promise.resolve();
    await Promise.race([
      Promise.resolve(fontsReady).catch(() => undefined),
      new Promise((resolve) => windowObject.setTimeout(resolve, FONT_WAIT_MS))
    ]);
    hide();
  };

  const remaining = Math.max(0, MAX_VISIBLE_MS - windowObject.performance.now());
  hardTimer = windowObject.setTimeout(remove, remaining);
  if (documentObject.readyState === "loading") {
    windowObject.addEventListener("DOMContentLoaded", finishAfterFonts, { once: true });
  } else {
    finishAfterFonts();
  }
}
