import { useState, useEffect } from "react";
export function useTheme() {
  const [dark, setDark] = useState(
    document.documentElement.dataset.theme === "dark",
  );
  useEffect(() => {
    const system = matchMedia("(prefers-color-scheme: dark)");
    const update = (event: MediaQueryListEvent) => {
      try {
        if (localStorage.getItem("raymond-theme")) return;
      } catch {}
      document.documentElement.dataset.theme = event.matches ? "dark" : "light";
      setDark(event.matches);
    };
    system.addEventListener("change", update);
    return () => system.removeEventListener("change", update);
  }, []);
  return {
    dark,
    toggle: () => {
      const value = !dark;
      setDark(value);
      document.documentElement.dataset.theme = value ? "dark" : "light";
      try {
        localStorage.setItem("raymond-theme", value ? "dark" : "light");
      } catch {}
    },
  };
}
