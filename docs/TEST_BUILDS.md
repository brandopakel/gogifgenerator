# Testing GoGIF builds

GoGIF's web app and API ship together in one server binary. The iPhone build is the installable web app served by that binary; iOS does not run the Go server itself.

## Build everything

```sh
make builds
```

This creates macOS, Windows, and Linux server binaries and Chrome, Edge, and Firefox local-extension ZIPs under `dist/`. `dist/SHA256SUMS` records the build checksums. These are unsigned test builds, not store releases.

## Test on this Mac

```sh
./dist/gogif-darwin-arm64
```

Open <http://localhost:8080>. Use **Edit** to choose a JPEG, PNG, existing GIF, or short video; drag the crop/caption guides or use the keyboard; choose an export preset; then Copy, Share, or Download. Uploaded source bytes are limited to 20 MiB and discarded when the request completes. A draft persists only when you explicitly choose **Save draft**, and stays in that browser's IndexedDB until deleted.

In **Search**, typing automatically refreshes results after a short pause and deleting the entire query clears them. **Actual GIFs** searches GIPHY when `GIPHY_API_KEY` is configured, otherwise it falls back to the archival GifCities catalog. Scrolling near the end continuously requests the next provider page on desktop and mobile. The displayed GIPHY rendition is an animated GIF rather than WebP; touch and hold the image itself or choose **Open GIF**. **Source clips & images** keeps Wikimedia, Prelinger, and NASA research media separate from finished GIF results.

**Stickers** uses GIPHY's dedicated sticker endpoint and keeps those transparent results separate from ordinary GIFs. It requires the same configured GIPHY platform key.

Photo and GIF editing needs no additional software. Short-video trim is enabled automatically when `ffmpeg` is available on the server Mac's `PATH`; otherwise the app shows the capability as unavailable while every other editor feature keeps working. Install FFmpeg with your preferred package manager, or set `GOGIF_FFMPEG_EXECUTABLE` to its absolute executable path, then restart GoGIF. The selected clip is limited to 15 seconds and temporary video files are deleted after each request.

## Test on iPhone

Web sharing files and installing a reliable Home Screen web app require a secure HTTPS origin. The simplest private test route for the existing Mac/iPhone Tailscale setup is:

1. Run `./dist/gogif-darwin-arm64` on the Mac.
2. Run `tailscale serve 8080` on the Mac and copy the HTTPS `*.ts.net` URL it prints.
3. Make sure the iPhone is connected to the same tailnet, then open that HTTPS URL in Safari.
4. In Safari's Share menu, choose **Add to Home Screen**.
5. Open GoGIF from the Home Screen, create or edit a GIF, select **Share**, and choose Messages.

Tailscale Serve remains private to the tailnet and terminates HTTPS in front of the loopback GoGIF server. Stop the foreground `tailscale serve` command with Control-C, or inspect/reset persistent configuration with `tailscale serve status` and `tailscale serve reset`.

On iPhone, test choosing media from Photos/Files, dragging the crop guide, saving/loading a draft, and exporting with the Messages preset. After export, press and hold directly on the resulting GIF and try **Copy**, then paste into Messages. Also choose Messages from GoGIF's **Share** button. Safari's native image menu is enabled for the result. When a browser rejects animated GIF clipboard/file sharing, Copy or Share falls back to a same-origin GIF link; zero-config links last for the current server session. Download remains the final reliable fallback. NASA and Prelinger search videos remain provider-hosted previews and are not silently imported into the editor.

## Test the desktop extension

Start the GoGIF server on `http://localhost:8080` first.

For Chrome or Edge, unpack the matching ZIP, open the browser's extensions page, enable developer mode, choose **Load unpacked**, and select the unpacked directory. The checked-in `apps/extension` directory can also be loaded directly.

For Firefox, open `about:debugging`, choose **This Firefox**, select **Load Temporary Add-on**, and select either the Firefox ZIP or its `manifest.json`. Temporary Firefox add-ons are removed when Firefox restarts; signed distribution is a later release task.

The local extension sends prompts only to the loopback GoGIF server and includes a link to the full editor. Browser-store signing, production HTTPS origins, and store-specific artwork validation are intentionally outside these local test packages.

## Native Messages extension boundary

The web app's **Share** button sends a generated GIF through the iOS share sheet, where Messages can receive it. A GoGIF interface embedded inside a Messages conversation is a separate native iMessage app extension. That target requires Xcode, an Apple bundle identity/signing team, an iOS container app or standalone iMessage app, and TestFlight/App Store distribution; it is not bundled into these web builds.
