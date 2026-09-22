# gitezz

Desktop Git multi-repo manager built with [Fyne](https://fyne.io/). Point it at a folder of repositories and pull, push, commit, or switch branches from one window.

## Requirements

- Go 1.22+ (module targets Go 1.25)
- Linux / macOS / Windows
- System `git` (used for **stash** and `git credential`; bundled inside the Flatpak build)
- SSH access to your remotes (agent or `~/.ssh/id_*` keys), or HTTPS credentials via `git credential`

## Install & run

```bash
git clone git@github.com:Spritan/gitezz.git
cd gitezz
go run ./cmd/main
```

Or build a binary:

```bash
go build -o gitezz ./cmd/main
./gitezz
```

### Linux Flatpak

Requires `flatpak` and `flatpak-builder`. The Flatpak bundles `git`, uses the host native folder picker (`kdialog`/`zenity` via `flatpak-spawn`), and needs home + SSH access for repos and remotes.

```bash
# one-time runtimes
flatpak install -y --user flathub \
  org.freedesktop.Platform//25.08 \
  org.freedesktop.Sdk//25.08 \
  org.freedesktop.Sdk.Extension.golang//25.08

# build and install for the current user
cd flatpak
flatpak-builder --user --install --force-clean build-dir com.github.spritan.gitezz.yml
flatpak run com.github.spritan.gitezz
```

Optional single-file bundle to share:

```bash
cd flatpak
flatpak-builder --repo=repo --force-clean build-dir com.github.spritan.gitezz.yml
flatpak build-bundle repo gitezz.flatpak com.github.spritan.gitezz
```

### macOS app & DMG

Requires [Fyne CLI](https://docs.fyne.io/started/packaging/) and Xcode Command Line Tools. From the repo root:

```bash
go install fyne.io/tools/cmd/fyne@latest

cd cmd/main
fyne package -os darwin
```

That creates `gitezz.app` (double-clickable). To wrap it in a DMG with a drag-to-Applications layout:

```bash
cd cmd/main

rm -rf dmg-root
mkdir dmg-root
cp -R gitezz.app dmg-root/
ln -s /Applications dmg-root/Applications

hdiutil create \
  -volname "gitezz" \
  -srcfolder dmg-root \
  -ov -format UDZO \
  gitezz-macos-arm64.dmg

rm -rf dmg-root
```

Open the DMG and drag **gitezz** into **Applications**.

Notes:

- The default build is for your Mac’s architecture (Apple Silicon → `arm64`). For Intel Macs, cross-build or produce a universal binary separately.
- Unsigned / adhoc-signed apps may be blocked by Gatekeeper. Recipients can right-click → **Open**, or run `xattr -cr gitezz.app` after copying out of the DMG.
- Optional: place an `Icon.png` (e.g. 1024×1024) in `cmd/main` so `fyne package` embeds an app icon (`FyneApp.toml` already sets name, ID, and version).

Optional debug logging:

```bash
GITEZZ_DEBUG=1 go run ./cmd/main
```

## How to use

1. Click **Select Folder** and choose a directory that contains Git repos (each subfolder with a `.git` directory is listed).
2. Wait for the scan + origin fetch (progress shows in the footer / overlay).
3. Use the table:
   - **Repository** — open the detail side panel
   - **Branch** — switch branches (stash / force options if dirty)
   - **Origin** — ahead / behind / dirty vs upstream
   - **Pull / Push / Commit** — per-repo actions
   - **Status / Logs** — last operation result
4. Toolbar: **Pull Selected**, **Pull All**, **Push Selected**, **Push All**, **Refresh**.
5. Detail panel: stage / unstage / discard, stash, commit, history, fetch / pull / push for the focused repo.
6. Drag header column handles to resize columns. Close the detail panel with **×** (splitter only appears while a panel is open).

## Tips

- Remotes honor your git `url.*.insteadOf` rules (e.g. HTTPS → SSH).
- Force-push is only available from the conflict / push-rejected dialog after confirmation.
- Stash still shells out to system `git stash` (bundled `git` in Flatpak).

## License

MIT — see [LICENSE](LICENSE).
