# GoGIF local browser extension

This Manifest V3 extension is a local testing surface for the same Go API used by the PWA. It targets `http://localhost:8080` so it can be exercised immediately with `make run`. The same package works as an unpacked Chrome or Edge extension and as a temporary Firefox add-on.

Run `make builds` to produce browser-labelled ZIP packages under `dist/`. Chrome and Edge can load the unpacked `apps/extension` directory directly. Firefox development testing uses `about:debugging` → **This Firefox** → **Load Temporary Add-on**, then selects `manifest.json`.

These local packages are not store releases or signed Firefox add-ons. Before store submission, inject the production HTTPS API origin, narrow host permissions to that origin, validate the raster icons against each store's artwork rules, sign the packages, and run store compatibility checks.
