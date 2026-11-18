# ui/overlay

Customer overlay skeleton (REQ-UI-001). The build renders a React component to static HTML and bundles a local browser module that hydrates it: a text field, empty message region, and disabled send and push-to-talk controls. No server state, response, progress or API request is fabricated. The named palette and type scale are in `src/tokens.css`. The UI-only build uses esbuild 0.28.2, approved in noema#696; it requires no runtime CDN.

Run `npm test` with Node 22.22.0 and a pre-installed Chrome or Chromium; this builds the page, runs the Node tests and the Playwright flows. The Playwright fixture runs the DOM scan after each flow. The test-only scanner checks text, attributes and current form values for Unicode decimal digits separated by the set used in `record/reconstruct/project.go` and selected literal leak indicators; it cannot identify all personal data or prove a response safe. It is not shipped with the static page. Tests use synthetic text. The browser build test loads the assets from a local static server, not the core.

Open: live routes (EL), integration of reconnection, actual messages and progress, complete privacy checks, accessibility verification and the Android US-01 run. No voice functionality is wired.
