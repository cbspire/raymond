import { createRoot } from "react-dom/client";
import { setNonce } from "get-nonce";
import { App } from "./app";
import "./styles.css";
const nonce = document.querySelector<HTMLMetaElement>(
  'meta[name="csp-nonce"]',
)?.content;
if (nonce) setNonce(nonce);
createRoot(document.getElementById("root")!).render(<App />);
