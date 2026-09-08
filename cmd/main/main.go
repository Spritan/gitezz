package main

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/lmittmann/tint"
	"github.com/ncruces/zenity"

	"github.com/Spritan/gitezz/internal/gitx"
)

type Repo struct {
	Path       string
	Name       string
	Branch     string
	Sync       gitx.Sync
	LastStatus string
	LastKind   widget.Importance
	LastLog    string
}

type appTheme struct{}

func (appTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 16, G: 16, B: 20, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 42, G: 44, B: 52, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 32, G: 33, B: 38, A: 255}
	case theme.ColorNameHover:
		return color.NRGBA{R: 54, G: 57, B: 68, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 28, G: 29, B: 36, A: 255}
	case theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 24, G: 25, B: 32, A: 255}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 48, G: 50, B: 58, A: 255}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 88, G: 166, B: 255, A: 255}
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 63, G: 185, B: 80, A: 255}
	case theme.ColorNameError:
		return color.NRGBA{R: 248, G: 81, B: 73, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 230, G: 237, B: 243, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 139, G: 148, B: 158, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 110, G: 118, B: 129, A: 255}
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (appTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (appTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (appTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameText:
		return 13
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameHeadingText:
		return 16
	case theme.SizeNameInlineIcon:
		return 16
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameScrollBar:
		return 8
	}
	return theme.DefaultTheme().Size(name)
}

const allBranches = "All branches"

var (
	appWindow fyne.Window
	rootPath  string

	reposMu sync.Mutex
	repos   []Repo

	repoContainer *fyne.Container
	tableStack    *fyne.Container
	statusLabel   *widget.Label
	busyBar       *widget.ProgressBarInfinite
	scanOverlay   *fyne.Container
	folderLabel   *widget.Label
	searchEntry   *widget.Entry
	branchFilter  *widget.Select
	selectAllCheck *widget.Check
	countLabel    *widget.Label

	mainBody      *fyne.Container
	mainSplit     *container.Split
	tablePane     fyne.CanvasObject
	detailWrapper *fyne.Container
	detailOpen    bool
	scanBusy      bool

	searchQuery    string
	branchQuery    = allBranches
	updatingChecks bool

	rowMu      sync.Mutex
	rowStatus  map[string]*widget.Label
	rowSync    map[string]*widget.Label
	rowChecks  map[string]*widget.Check
	rowLogs    map[string]*widget.Button
	selectedMu sync.Mutex
	selected   = map[string]bool{}
)

func previousStatus() map[string]Repo {
	reposMu.Lock()
	defer reposMu.Unlock()

	prev := make(map[string]Repo, len(repos))
	for _, r := range repos {
		prev[r.Path] = r
	}
	return prev
}

func findRepos(root string) []Repo {
	var result []Repo
	prev := previousStatus()

	entries, err := os.ReadDir(root)
	if err != nil {
		slog.Error("read folder failed", "path", root, "err", err)
		return result
	}

	slog.Info("scanning folder", "path", root, "entries", len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			slog.Debug("skip non-git dir", "path", path)
			continue
		}

		branch := gitx.CurrentBranch(path)
		sync := gitx.UpstreamSync(path)
		repo := Repo{
			Path:   path,
			Name:   entry.Name(),
			Branch: branch,
			Sync:   sync,
		}

		if old, ok := prev[path]; ok {
			repo.LastStatus = old.LastStatus
			repo.LastKind = old.LastKind
			repo.LastLog = old.LastLog
		}

		slog.Info("found repo",
			"name", repo.Name,
			"branch", repo.Branch,
			"sync", repo.Sync.String(),
		)
		result = append(result, repo)
	}

	slog.Info("scan complete", "repos", len(result), "path", root)
	return result
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		ms := d.Milliseconds()
		if ms < 1 {
			ms = 1
		}
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func setFooter(text string) {
	fyne.Do(func() {
		if statusLabel != nil {
			statusLabel.SetText(text)
		}
	})
}

// setScanBusy shows/hides the footer loader. msg updates the footer when non-empty.
func setScanBusy(on bool, msg string) {
	fyne.Do(func() {
		scanBusy = on
		if msg != "" && statusLabel != nil {
			statusLabel.SetText(msg)
		}
		if busyBar != nil {
			if on {
				busyBar.Show()
				busyBar.Start()
			} else {
				busyBar.Stop()
				busyBar.Hide()
			}
		}
	})
}

func setScanOverlay(on bool) {
	fyne.Do(func() {
		if scanOverlay == nil {
			return
		}
		if on {
			scanOverlay.Show()
		} else {
			scanOverlay.Hide()
		}
		if tableStack != nil {
			tableStack.Refresh()
		}
	})
}

func setRowStatus(path string, label *widget.Label, text string, kind widget.Importance) {
	fyne.Do(func() {
		if label != nil {
			label.SetText(text)
			label.Importance = kind
			label.Refresh()
		}
	})

	reposMu.Lock()
	for i := range repos {
		if repos[i].Path == path {
			repos[i].LastStatus = text
			repos[i].LastKind = kind
			break
		}
	}
	reposMu.Unlock()
}

func setRowLog(path string, logText string) {
	reposMu.Lock()
	for i := range repos {
		if repos[i].Path == path {
			repos[i].LastLog = logText
			break
		}
	}
	reposMu.Unlock()

	fyne.Do(func() {
		rowMu.Lock()
		btn := rowLogs[path]
		rowMu.Unlock()
		if btn == nil {
			return
		}
		if strings.TrimSpace(logText) == "" {
			btn.Hide()
		} else {
			btn.Show()
		}
		btn.Refresh()
	})
}

func showRepoLog(r Repo, title string) {
	text := r.LastLog
	reposMu.Lock()
	for _, repo := range repos {
		if repo.Path == r.Path {
			text = repo.LastLog
			break
		}
	}
	reposMu.Unlock()
	if strings.TrimSpace(text) == "" {
		text = "(no log)"
	}

	entry := widget.NewMultiLineEntry()
	entry.SetText(text)
	entry.Wrapping = fyne.TextWrapWord

	scroll := container.NewScroll(entry)
	scroll.SetMinSize(fyne.NewSize(520, 280))

	d := dialog.NewCustom(title, "Close", scroll, appWindow)
	d.Resize(fyne.NewSize(560, 360))
	d.Show()
}

func finishPullOK(r Repo, status *widget.Label, out string, elapsed string) {
	summary := gitx.SummarizePull(out)
	line := summary + " · " + elapsed
	setRowStatus(r.Path, status, line, widget.SuccessImportance)
	setRowLog(r.Path, out)
	setFooter(fmt.Sprintf("%s: %s", r.Name, summary))
	clearAttention(r.Path)
	updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
	if detailOpen && activeDetailPath == r.Path {
		fyne.Do(func() { reloadDetailPanel(r) })
	}
}

func applySyncStyle(lbl *widget.Label, sync gitx.Sync) {
	if lbl == nil {
		return
	}
	lbl.SetText(sync.String())
	switch {
	case !sync.HasUpstream:
		lbl.Importance = widget.MediumImportance
	case sync.Diverged():
		lbl.Importance = widget.WarningImportance
	case sync.Dirty && (sync.Ahead > 0 || sync.Behind > 0):
		lbl.Importance = widget.WarningImportance
	case sync.Dirty:
		lbl.Importance = widget.WarningImportance
	case sync.Behind > 0:
		lbl.Importance = widget.DangerImportance
	case sync.Ahead > 0:
		lbl.Importance = widget.HighImportance
	default:
		lbl.Importance = widget.SuccessImportance
	}
	lbl.Refresh()
}

func updateRepoSync(path string, sync gitx.Sync) {
	reposMu.Lock()
	for i := range repos {
		if repos[i].Path == path {
			repos[i].Sync = sync
			repos[i].Branch = gitx.CurrentBranch(path)
			break
		}
	}
	reposMu.Unlock()

	fyne.Do(func() {
		rowMu.Lock()
		lbl := rowSync[path]
		rowMu.Unlock()
		applySyncStyle(lbl, sync)
	})
}

func rowStatusFor(path string) *widget.Label {
	rowMu.Lock()
	defer rowMu.Unlock()
	return rowStatus[path]
}

func isSelected(path string) bool {
	selectedMu.Lock()
	defer selectedMu.Unlock()
	return selected[path]
}

func setSelected(path string, on bool) {
	selectedMu.Lock()
	defer selectedMu.Unlock()
	if on {
		selected[path] = true
		return
	}
	delete(selected, path)
}

func matchesFilter(r Repo) bool {
	q := strings.ToLower(strings.TrimSpace(searchQuery))
	if q != "" {
		if !strings.Contains(strings.ToLower(r.Name), q) &&
			!strings.Contains(strings.ToLower(r.Branch), q) {
			return false
		}
	}
	if branchQuery != "" && branchQuery != allBranches && r.Branch != branchQuery {
		return false
	}
	return true
}

func visibleRepos() []Repo {
	reposMu.Lock()
	current := make([]Repo, len(repos))
	copy(current, repos)
	reposMu.Unlock()

	var visible []Repo
	for _, r := range current {
		if matchesFilter(r) {
			visible = append(visible, r)
		}
	}
	return visible
}

func uniqueBranches() []string {
	reposMu.Lock()
	defer reposMu.Unlock()

	seen := map[string]struct{}{}
	opts := []string{allBranches}
	for _, r := range repos {
		if r.Branch == "" || r.Branch == "?" {
			continue
		}
		if _, ok := seen[r.Branch]; ok {
			continue
		}
		seen[r.Branch] = struct{}{}
		opts = append(opts, r.Branch)
	}
	return opts
}

type repoColumns struct{}

func (c repoColumns) MinSize(objects []fyne.CanvasObject) fyne.Size {
	w, h := float32(0), float32(0)
	for _, o := range objects {
		m := o.MinSize()
		w += m.Width
		if m.Height > h {
			h = m.Height
		}
	}
	return fyne.NewSize(w+40, h+8)
}

func (c repoColumns) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 6 {
		return
	}

	content := contentColWidths(size.Width)
	widths := []float32{colCheckW, content[0], content[1], content[2], content[3], content[4]}

	x := float32(0)
	for i, o := range objects {
		w := widths[i]
		m := o.MinSize()
		h := m.Height
		if i == 2 || i == 4 {
			h = min(max(m.Height, 28), size.Height)
		}
		y := (size.Height - h) / 2
		if y < 0 {
			y = 0
			h = size.Height
		}
		o.Move(fyne.NewPos(x, y))
		o.Resize(fyne.NewSize(w, h))
		x += w + colGap
	}
}

func tableRow(objects ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewPadded(container.New(repoColumns{}, objects...))
}

func createRepoRow(r Repo) fyne.CanvasObject {
	check := widget.NewCheck("", nil)
	check.SetChecked(isSelected(r.Path))
	check.OnChanged = func(on bool) {
		if updatingChecks {
			return
		}
		setSelected(r.Path, on)
		syncSelectAll()
	}

	nameBtn := widget.NewButton(r.Name, func() {
		if openAttentionIfAny(r) {
			return
		}
		openDetailPanel(r)
	})
	nameBtn.Alignment = widget.ButtonAlignLeading
	nameBtn.Importance = widget.LowImportance

	branchList := gitx.Branches(r.Path)
	branchSelect := widget.NewSelect(branchList, nil)
	branchSelect.PlaceHolder = "Branch"
	branchSelect.SetSelected(r.Branch)

	syncLbl := widget.NewLabel("")
	syncLbl.Truncation = fyne.TextTruncateEllipsis
	applySyncStyle(syncLbl, r.Sync)

	status := widget.NewLabel("—")
	status.Importance = widget.MediumImportance
	status.Truncation = fyne.TextTruncateEllipsis
	if r.LastStatus != "" {
		status.SetText(r.LastStatus)
		status.Importance = r.LastKind
	}

	logsBtn := widget.NewButton("Logs", func() {
		showRepoLog(r, r.Name+" — pull log")
	})
	logsBtn.Importance = widget.LowImportance
	if strings.TrimSpace(r.LastLog) == "" {
		logsBtn.Hide()
	}

	rowMu.Lock()
	rowStatus[r.Path] = status
	rowSync[r.Path] = syncLbl
	rowChecks[r.Path] = check
	rowLogs[r.Path] = logsBtn
	rowMu.Unlock()

	pullButton := widget.NewButtonWithIcon("Pull", theme.DownloadIcon(), func() {
		maybeWarnDiverged(r)
		pullRepo(r, status)
	})

	pushButton := widget.NewButtonWithIcon("Push", theme.UploadIcon(), func() {
		maybeWarnDiverged(r)
		showPushDialog([]Repo{r}, r.Name)
	})
	pushButton.Importance = widget.HighImportance

	commitButton := widget.NewButtonWithIcon("Commit", theme.ConfirmIcon(), func() {
		showCommitDialog(r)
	})

	actions := container.NewHBox(pullButton, pushButton, commitButton)
	statusCell := container.NewBorder(nil, nil, nil, logsBtn, status)

	branchSelect.OnChanged = func(branch string) {
		switchBranch(r, branch, status)
	}

	line := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	line.SetMinSize(fyne.NewSize(1, 1))

	return container.NewVBox(
		tableRow(check, nameBtn, branchSelect, syncLbl, actions, statusCell),
		line,
	)
}

func syncSelectAll() {
	visible := visibleRepos()
	if selectAllCheck == nil {
		return
	}

	allOn := len(visible) > 0
	for _, r := range visible {
		if !isSelected(r.Path) {
			allOn = false
			break
		}
	}

	updatingChecks = true
	selectAllCheck.SetChecked(allOn)
	updatingChecks = false
}

func updateCountLabel(n, total int) {
	if countLabel == nil {
		return
	}
	if n == total {
		countLabel.SetText(fmt.Sprintf("%d repositories", total))
		return
	}
	countLabel.SetText(fmt.Sprintf("%d of %d shown", n, total))
}

func renderRepos() {
	repoContainer.Objects = nil

	rowMu.Lock()
	rowStatus = make(map[string]*widget.Label)
	rowSync = make(map[string]*widget.Label)
	rowChecks = make(map[string]*widget.Check)
	rowLogs = make(map[string]*widget.Button)
	rowMu.Unlock()

	visible := visibleRepos()
	reposMu.Lock()
	total := len(repos)
	reposMu.Unlock()

	for _, r := range visible {
		repoContainer.Add(createRepoRow(r))
	}

	if branchFilter != nil {
		opts := uniqueBranches()
		branchFilter.Options = opts
		if branchQuery == "" {
			branchQuery = allBranches
		}
		updatingChecks = true
		branchFilter.SetSelected(branchQuery)
		updatingChecks = false
	}

	updateCountLabel(len(visible), total)
	syncSelectAll()
	repoContainer.Refresh()
}

func applyRepoList(found []Repo, statusText string) {
	reposMu.Lock()
	repos = found
	reposMu.Unlock()

	fyne.Do(func() {
		renderRepos()
		statusLabel.SetText(statusText)
		if folderLabel != nil && rootPath != "" {
			folderLabel.SetText(rootPath)
		}
		if detailOpen && activeDetailPath != "" {
			for _, r := range found {
				if r.Path == activeDetailPath {
					reloadDetailPanel(r)
					break
				}
			}
		}
	})
}

func refreshRepos() {
	if rootPath == "" {
		slog.Warn("refresh skipped: no folder selected")
		return
	}

	setScanBusy(true, "Scanning repositories…")
	setScanOverlay(true)
	slog.Info("refresh started", "path", rootPath)

	go func() {
		found := findRepos(rootPath)
		if len(found) == 0 {
			slog.Warn("no git repositories found", "path", rootPath)
			applyRepoList(found, "0 repositories found")
			setScanOverlay(false)
			setScanBusy(false, "0 repositories found")
			return
		}

		applyRepoList(found, fmt.Sprintf("%d repositories — fetching origin…", len(found)))
		setScanOverlay(false)
		setScanBusy(true, fmt.Sprintf("%d repositories — fetching origin…", len(found)))
		fetchOriginsAsync(found)
	}()
}

func fetchOriginsAsync(list []Repo) {
	slog.Info("fetching origin for repos", "count", len(list))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	var done atomic.Int32
	total := int32(len(list))

	for _, r := range list {
		wg.Add(1)
		go func(r Repo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			slog.Info("fetch origin", "repo", r.Name)
			sync, out, err := gitx.FetchAndSync(r.Path)
			n := done.Add(1)
			setFooter(fmt.Sprintf("Fetching origin… %d/%d — %s", n, total, r.Name))
			if err != nil {
				slog.Error("fetch origin failed",
					"repo", r.Name,
					"err", err,
					"out", gitx.FirstLine(out),
					"sync", sync.String(),
				)
			} else {
				slog.Info("fetch origin ok", "repo", r.Name, "sync", sync.String())
			}
			updateRepoSync(r.Path, sync)
		}(r)
	}

	go func() {
		wg.Wait()
		msg := fmt.Sprintf("%d repositories (origin fetched)", len(list))
		setScanBusy(false, msg)
		slog.Info("all origin fetches finished", "count", len(list))
	}()
}

func pullRepo(r Repo, status *widget.Label) {
	setRowStatus(r.Path, status, "Pulling…", widget.MediumImportance)

	go func() {
		start := time.Now()
		out, err := gitx.Pull(r.Path)
		elapsed := formatDuration(time.Since(start))

		if err != nil {
			setRowLog(r.Path, out)
			handleClassifiedFailure(r, gitx.OpPull, trimBody(out), status, elapsed, "", true)
			return
		}

		finishPullOK(r, status, out, elapsed)
	}()
}

func switchBranch(r Repo, branch string, status *widget.Label) {
	if branch == "" || branch == r.Branch {
		return
	}

	setRowStatus(r.Path, status, "Switching…", widget.MediumImportance)

	go func() {
		start := time.Now()
		out, err := gitx.Switch(r.Path, branch)
		elapsed := formatDuration(time.Since(start))

		if err != nil {
			handleClassifiedFailure(r, gitx.OpSwitch, trimBody(out), status, elapsed, branch, true)
			return
		}

		clearAttention(r.Path)
		finishSwitchOK(r, branch, status, elapsed)
	}()
}

func finishSwitchOK(r Repo, branch string, status *widget.Label, elapsed string) {
	setRowStatus(r.Path, status, "Switched in "+elapsed, widget.SuccessImportance)
	setFooter(fmt.Sprintf("%s → %s", r.Name, branch))
	updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
	if detailOpen && activeDetailPath == r.Path {
		fyne.Do(func() { reloadDetailPanel(r) })
	}
	fyne.Do(func() {
		reposMu.Lock()
		for i := range repos {
			if repos[i].Path == r.Path {
				repos[i].Branch = branch
				repos[i].Sync = gitx.UpstreamSync(r.Path)
				break
			}
		}
		reposMu.Unlock()
		renderRepos()
	})
}

func pullRepos(currentRepos []Repo, label string) {
	if len(currentRepos) == 0 {
		statusLabel.SetText("No repositories selected")
		return
	}

	for _, r := range currentRepos {
		if status := rowStatusFor(r.Path); status != nil {
			setRowStatus(r.Path, status, "Waiting…", widget.MediumImportance)
		}
	}

	statusLabel.SetText("Pulling " + label + "…")
	slog.Info("parallel pull started", "label", label, "count", len(currentRepos))

	go func() {
		start := time.Now()
		var failed atomic.Int32
		var attention []attentionItem
		var attentionMuLocal sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)

		for _, r := range currentRepos {
			wg.Add(1)
			go func(r Repo) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				status := rowStatusFor(r.Path)
				setRowStatus(r.Path, status, "Pulling…", widget.MediumImportance)

				repoStart := time.Now()
				out, err := gitx.Pull(r.Path)
				elapsed := formatDuration(time.Since(repoStart))

				if err != nil {
					failed.Add(1)
					setRowLog(r.Path, out)
					kind := handleClassifiedFailure(r, gitx.OpPull, trimBody(out), status, elapsed, "", false)
					if kind.Actionable() {
						attentionMuLocal.Lock()
						attention = append(attention, attentionItem{
							Repo: r, Kind: kind, Out: trimBody(out), Op: gitx.OpPull,
						})
						attentionMuLocal.Unlock()
					}
					return
				}

				finishPullOK(r, status, out, elapsed)
				slog.Info("pull ok", "repo", r.Name, "elapsed", elapsed, "summary", gitx.SummarizePull(out))
			}(r)
		}

		wg.Wait()
		total := formatDuration(time.Since(start))
		nFail := int(failed.Load())
		summary := fmt.Sprintf("Pull finished in %s (%d repos)", total, len(currentRepos))
		if nFail > 0 {
			summary = fmt.Sprintf("Pull finished in %s · %d failed", total, nFail)
		}
		setFooter(summary)
		slog.Info("parallel pull finished", "count", len(currentRepos), "failed", nFail, "elapsed", total)
		showAttentionSummary(attention)
	}()
}

func pullAll() {
	pullRepos(visibleRepos(), "visible repositories")
}

func pullSelected() {
	visible := visibleRepos()
	var chosen []Repo
	for _, r := range visible {
		if isSelected(r.Path) {
			chosen = append(chosen, r)
		}
	}
	pullRepos(chosen, "selected repositories")
}

func toggleSelectVisible(on bool) {
	visible := visibleRepos()
	updatingChecks = true
	for _, r := range visible {
		setSelected(r.Path, on)
		rowMu.Lock()
		check := rowChecks[r.Path]
		rowMu.Unlock()
		if check != nil {
			check.SetChecked(on)
		}
	}
	updatingChecks = false
	syncSelectAll()
}

func selectFolder() {
	go func() {
		opts := []zenity.Option{
			zenity.Title("Select folder of Git repositories"),
			zenity.Directory(),
		}
		if rootPath != "" {
			opts = append(opts, zenity.Filename(rootPath))
		}
		path, err := zenity.SelectFile(opts...)
		if err != nil {
			if err == zenity.ErrCanceled {
				slog.Info("folder dialog cancelled")
				return
			}
			slog.Error("folder dialog error", "err", err)
			fyne.Do(func() {
				setFooter("Folder picker failed: " + err.Error())
			})
			return
		}
		if strings.TrimSpace(path) == "" {
			slog.Info("folder dialog cancelled")
			return
		}

		fyne.Do(func() {
			rootPath = path
			slog.Info("folder selected", "path", rootPath)
			if folderLabel != nil {
				folderLabel.SetText(rootPath)
			}
			refreshRepos()
		})
	}()
}

func tableCard(inner fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{R: 24, G: 25, B: 32, A: 255})
	bg.CornerRadius = 8
	stroke := canvas.NewRectangle(color.Transparent)
	stroke.StrokeColor = color.NRGBA{R: 48, G: 50, B: 58, A: 255}
	stroke.StrokeWidth = 1
	stroke.CornerRadius = 8
	return container.NewPadded(container.NewStack(bg, stroke, container.NewPadded(inner)))
}

func initLogger() {
	level := slog.LevelInfo
	if os.Getenv("GITEZZ_DEBUG") == "1" {
		level = slog.LevelDebug
	}
	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:      level,
		TimeFormat: time.Kitchen,
	})
	slog.SetDefault(slog.New(handler))
	slog.Info("logger ready", "level", level.String())
}

func main() {
	initLogger()

	a := app.NewWithID("com.github.spritan.gitezz")
	a.Settings().SetTheme(appTheme{})

	appWindow = a.NewWindow("gitezz")
	appWindow.Resize(fyne.NewSize(1280, 760))
	slog.Info("starting gitezz")

	rowStatus = make(map[string]*widget.Label)
	rowSync = make(map[string]*widget.Label)
	rowChecks = make(map[string]*widget.Check)
	rowLogs = make(map[string]*widget.Button)
	repoContainer = container.NewVBox()

	title := widget.NewLabel("gitezz")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Importance = widget.HighImportance

	folderLabel = widget.NewLabel("No folder selected")
	folderLabel.Importance = widget.MediumImportance
	folderLabel.Truncation = fyne.TextTruncateEllipsis

	countLabel = widget.NewLabel("")
	countLabel.Importance = widget.MediumImportance

	selectButton := widget.NewButtonWithIcon("Select Folder", theme.FolderOpenIcon(), selectFolder)
	refreshButton := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), refreshRepos)
	pullSelectedButton := widget.NewButton("Pull Selected", pullSelected)
	pullAllButton := widget.NewButtonWithIcon("Pull All", theme.DownloadIcon(), pullAll)
	pullAllButton.Importance = widget.HighImportance
	pushSelectedButton := widget.NewButton("Push Selected", pushSelected)
	pushAllButton := widget.NewButtonWithIcon("Push All", theme.UploadIcon(), pushAll)
	pushAllButton.Importance = widget.HighImportance

	toolbar := container.NewBorder(
		nil,
		nil,
		title,
		container.NewHBox(
			selectButton, refreshButton,
			pullSelectedButton, pullAllButton,
			pushSelectedButton, pushAllButton,
		),
		folderLabel,
	)

	searchEntry = widget.NewEntry()
	searchEntry.SetPlaceHolder("Search repositories or branches")
	searchEntry.OnChanged = func(q string) {
		searchQuery = q
		renderRepos()
	}

	branchFilter = widget.NewSelect([]string{allBranches}, func(v string) {
		if updatingChecks {
			return
		}
		branchQuery = v
		renderRepos()
	})
	branchFilter.SetSelected(allBranches)

	filterBar := container.NewBorder(
		nil,
		nil,
		nil,
		container.NewHBox(branchFilter, countLabel),
		searchEntry,
	)

	selectAllCheck = widget.NewCheck("", func(on bool) {
		if updatingChecks {
			return
		}
		toggleSelectVisible(on)
	})

	headerRow = tableRow(
		selectAllCheck,
		headerCell("Repository", 0),
		headerCell("Branch", 1),
		headerCell("Origin", 2),
		headerCell("Actions", 3),
		headerCell("Status", -1),
	)

	headerLine := canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	headerLine.SetMinSize(fyne.NewSize(1, 1))

	scanMsg := widget.NewLabel("Scanning repositories…")
	scanMsg.Alignment = fyne.TextAlignCenter
	scanMsg.TextStyle = fyne.TextStyle{Bold: true}
	scanMsg.Importance = widget.MediumImportance
	overlayBar := widget.NewProgressBarInfinite()
	overlayBar.Start()
	scanOverlay = container.NewCenter(container.NewVBox(
		scanMsg,
		container.NewPadded(overlayBar),
	))
	scanOverlay.Hide()

	tableInner := tableCard(container.NewBorder(
		container.NewVBox(headerRow, headerLine),
		nil,
		nil,
		nil,
		container.NewVScroll(repoContainer),
	))
	tableStack = container.NewStack(tableInner, scanOverlay)
	table := tableStack

	detailWrapper = container.NewStack()
	detailOpen = false
	tablePane = table
	// No HSplit until a repo detail panel is open — otherwise the drag handle stays visible.
	mainBody = container.NewStack(tablePane)

	statusLabel = widget.NewLabel("Select a folder of Git repositories")
	statusLabel.Wrapping = fyne.TextWrapOff
	statusLabel.Truncation = fyne.TextTruncateEllipsis
	statusLabel.Importance = widget.MediumImportance

	busyBar = widget.NewProgressBarInfinite()
	busyBar.Hide()

	footer := container.NewBorder(nil, nil, nil, busyBar, statusLabel)

	content := container.NewBorder(
		container.NewVBox(
			container.NewPadded(toolbar),
			container.NewPadded(filterBar),
		),
		container.NewPadded(footer),
		nil,
		nil,
		mainBody,
	)

	appWindow.SetContent(content)
	appWindow.ShowAndRun()
}
