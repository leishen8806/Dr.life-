(function () {
  var reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var compactViewport = window.matchMedia("(max-width: 700px)").matches;
  var seen = false;

  try {
    seen = window.sessionStorage.getItem("drlife-loading-intro-seen") === "1";
  } catch (_error) {
    seen = false;
  }

  if (reducedMotion || compactViewport || seen) return;

  document.documentElement.classList.add("intro");
  try {
    window.sessionStorage.setItem("drlife-loading-intro-seen", "1");
  } catch (_error) {
    // The first paint must not depend on storage availability.
  }
}());
