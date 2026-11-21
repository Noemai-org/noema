import { createElement } from "react";
import { hydrateRoot } from "react-dom/client";
import { Overlay } from "./overlay.js";

const app = document.getElementById("app");
if (app) hydrateRoot(app, createElement(Overlay));
