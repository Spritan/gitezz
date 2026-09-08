# gitezz

Desktop Git multi-repo manager built with [Fyne](https://fyne.io/). Point it at a folder of repositories and pull, push, commit, or switch branches from one window.

## Requirements

- Go 1.22+ (tested with Go 1.26)
- Linux / macOS / Windows
- System `git` (used for **stash** only)
- SSH access to your remotes (agent or `~/.ssh/id_*` keys), or HTTPS credentials via `git credential`
- [zenity](https://github.com/ncruces/zenity)-compatible dialogs for the folder picker (native on Linux; zenity Go package handles Windows/macOS)

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
- Stash still shells out to system `git stash`.

## License

MIT — see [LICENSE](LICENSE).
