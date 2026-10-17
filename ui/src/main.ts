import { SHELL_STATUS, SHELL_TITLE } from "./shell.js";

const root = document.getElementById("app");
if (root !== null) {
  const h1 = document.createElement("h1");
  h1.textContent = SHELL_TITLE;
  const p = document.createElement("p");
  p.textContent = SHELL_STATUS;
  root.append(h1, p);
}
