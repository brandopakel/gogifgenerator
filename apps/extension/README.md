# GoGIF browser extension shell

This unpacked Manifest V3 extension is a development surface for the same Go API used by the PWA. It targets `http://localhost:8080` so it can be exercised immediately with `make run`.

Before store submission, add a packaging step that injects the production HTTPS API origin, narrows host permissions to that origin, supplies store-ready raster icons, and runs Chrome/Edge/Firefox compatibility checks. Do not ship the `Dev` name or localhost permission as the production package.
