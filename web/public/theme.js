// Run before rendering to avoid a light-theme flash. React owns the toggle.
(() => {
  let saved;
  try {
    saved = localStorage.getItem("raymond-theme");
  } catch {}
  document.documentElement.dataset.theme =
    saved === "dark" ||
    (saved !== "light" && matchMedia("(prefers-color-scheme: dark)").matches)
      ? "dark"
      : "light";
})();
