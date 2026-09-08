package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/Spritan/gitezz/internal/gitx"
)

var (
	activeDetailPath   string
	detailTitle        *widget.Label
	detailBranch       *widget.Label
	detailSync         *widget.Label
	detailChanges      *fyne.Container
	detailHistory      *fyne.Container
	detailCommitMsg    *widget.Entry
	detailStashMsg     *widget.Entry
	detailStageAll     *widget.Check
	detailSelectAll    *widget.Check
	detailFileChecks   map[string]*widget.Check
	detailFiles        []gitx.FileChange
	detailPanelRoot    fyne.CanvasObject
	detailUpdatingAll  bool
)

func openDetailPanel(r Repo) {
	activeDetailPath = r.Path
	if !detailOpen {
		detailOpen = true
		if detailPanelRoot == nil {
			detailPanelRoot = buildDetailPanel()
		}
		detailWrapper.Objects = []fyne.CanvasObject{detailPanelRoot}
		detailWrapper.Refresh()
		mainSplit = container.NewHSplit(tablePane, detailWrapper)
		mainSplit.Offset = 0.55
		if mainBody != nil {
			mainBody.Objects = []fyne.CanvasObject{mainSplit}
			mainBody.Refresh()
		}
	}
	reloadDetailPanel(r)
}

func closeDetailPanel() {
	activeDetailPath = ""
	detailOpen = false
	detailWrapper.Objects = nil
	detailWrapper.Refresh()
	mainSplit = nil
	if mainBody != nil && tablePane != nil {
		mainBody.Objects = []fyne.CanvasObject{tablePane}
		mainBody.Refresh()
	}
}

func buildDetailPanel() fyne.CanvasObject {
	detailTitle = widget.NewLabel("Repository")
	detailTitle.TextStyle = fyne.TextStyle{Bold: true}
	detailTitle.Importance = widget.HighImportance

	detailBranch = widget.NewLabel("")
	detailBranch.Importance = widget.MediumImportance

	detailSync = widget.NewLabel("")
	detailSync.Importance = widget.MediumImportance

	fetchBtn := widget.NewButtonWithIcon("Fetch", theme.ViewRefreshIcon(), func() { detailFetch() })
	pullBtn := widget.NewButtonWithIcon("Pull", theme.DownloadIcon(), func() { detailPull() })
	pushBtn := widget.NewButtonWithIcon("Push", theme.UploadIcon(), func() { detailPush() })
	pushBtn.Importance = widget.HighImportance
	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), closeDetailPanel)
	closeBtn.Importance = widget.LowImportance

	actions := container.NewHBox(fetchBtn, pullBtn, pushBtn)
	headerRight := container.NewHBox(actions, widget.NewSeparator(), closeBtn)
	headerLeft := container.NewHBox(detailTitle, detailBranch, detailSync)

	header := container.NewBorder(nil, nil, nil, headerRight, headerLeft)

	detailChanges = container.NewVBox()
	detailHistory = container.NewVBox()
	detailFileChecks = map[string]*widget.Check{}

	detailSelectAll = widget.NewCheck("Select all", func(on bool) {
		if detailUpdatingAll {
			return
		}
		detailUpdatingAll = true
		for _, check := range detailFileChecks {
			if check != nil {
				check.SetChecked(on)
			}
		}
		detailUpdatingAll = false
	})

	stageBtn := widget.NewButtonWithIcon("Stage", theme.ContentAddIcon(), func() { detailStage(true) })
	unstageBtn := widget.NewButtonWithIcon("Unstage", theme.ContentRemoveIcon(), func() { detailStage(false) })
	discardBtn := widget.NewButtonWithIcon("Discard", theme.DeleteIcon(), func() { detailDiscard() })
	discardBtn.Importance = widget.DangerImportance

	detailStashMsg = widget.NewEntry()
	detailStashMsg.SetPlaceHolder("Stash message (optional)")
	stashBtn := widget.NewButtonWithIcon("Stash", theme.DocumentSaveIcon(), func() { detailStash() })

	changeActions := container.NewHBox(detailSelectAll, stageBtn, unstageBtn, discardBtn, stashBtn)

	detailCommitMsg = widget.NewMultiLineEntry()
	detailCommitMsg.SetPlaceHolder("Commit message")
	detailCommitMsg.SetMinRowsVisible(3)
	detailStageAll = widget.NewCheck("Stage all before commit", nil)
	commitBtn := widget.NewButtonWithIcon("Commit", theme.ConfirmIcon(), func() { detailCommit() })
	commitBtn.Importance = widget.HighImportance

	commitBox := container.NewVBox(
		widget.NewLabelWithStyle("Commit", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		detailCommitMsg,
		detailStageAll,
		commitBtn,
	)

	changesSection := container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Changes", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			changeActions,
			detailStashMsg,
		),
		nil, nil, nil,
		container.NewVScroll(detailChanges),
	)

	historySection := container.NewBorder(
		widget.NewLabelWithStyle("History", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		container.NewVScroll(detailHistory),
	)

	body := container.NewVSplit(
		changesSection,
		container.NewVSplit(commitBox, historySection),
	)
	body.Offset = 0.45

	return tableCard(container.NewBorder(
		container.NewVBox(header, widget.NewSeparator()),
		nil, nil, nil,
		body,
	))
}

func syncDetailSelectAll() {
	if detailSelectAll == nil {
		return
	}
	if len(detailFileChecks) == 0 {
		detailUpdatingAll = true
		detailSelectAll.SetChecked(false)
		detailUpdatingAll = false
		return
	}
	allOn := true
	for _, check := range detailFileChecks {
		if check == nil || !check.Checked {
			allOn = false
			break
		}
	}
	detailUpdatingAll = true
	detailSelectAll.SetChecked(allOn)
	detailUpdatingAll = false
}

func reloadDetailPanel(r Repo) {
	if detailTitle == nil {
		return
	}
	r.Branch = gitx.CurrentBranch(r.Path)
	r.Sync = gitx.UpstreamSync(r.Path)

	detailTitle.SetText(r.Name)
	detailBranch.SetText("·  " + r.Branch)
	applySyncStyle(detailSync, r.Sync)
	detailSync.SetText("·  " + r.Sync.String())
	detailSync.Refresh()

	files, err := gitx.StatusPorcelain(r.Path)
	detailFiles = files
	detailChanges.Objects = nil
	detailFileChecks = map[string]*widget.Check{}

	if err != nil {
		detailChanges.Add(widget.NewLabel("Failed to load status: " + err.Error()))
	} else if len(files) == 0 {
		detailChanges.Add(widget.NewLabel("No local changes"))
	} else {
		for _, f := range files {
			f := f
			check := widget.NewCheck(f.Label(), func(_ bool) {
				if detailUpdatingAll {
					return
				}
				syncDetailSelectAll()
			})
			detailFileChecks[f.Path] = check
			detailChanges.Add(check)
		}
	}
	syncDetailSelectAll()
	detailChanges.Refresh()

	detailHistory.Objects = nil
	logs, err := gitx.Log(r.Path, 30)
	if err != nil {
		detailHistory.Add(widget.NewLabel("Failed to load history"))
	} else if len(logs) == 0 {
		detailHistory.Add(widget.NewLabel("No commits"))
	} else {
		for _, e := range logs {
			lbl := widget.NewLabel(fmt.Sprintf("%s  %s", e.Hash, e.Subject))
			lbl.TextStyle = fyne.TextStyle{Monospace: true}
			lbl.Truncation = fyne.TextTruncateEllipsis
			detailHistory.Add(lbl)
		}
	}
	detailHistory.Refresh()
}

func activeRepo() (Repo, bool) {
	if activeDetailPath == "" {
		return Repo{}, false
	}
	reposMu.Lock()
	defer reposMu.Unlock()
	for _, r := range repos {
		if r.Path == activeDetailPath {
			return r, true
		}
	}
	return Repo{Path: activeDetailPath, Name: activeDetailPath}, true
}

func selectedDetailPaths() []string {
	var paths []string
	for path, check := range detailFileChecks {
		if check != nil && check.Checked {
			paths = append(paths, path)
		}
	}
	return paths
}

func detailFetch() {
	r, ok := activeRepo()
	if !ok {
		return
	}
	go func() {
		start := time.Now()
		sync, out, err := gitx.FetchAndSync(r.Path)
		elapsed := formatDuration(time.Since(start))
		updateRepoSync(r.Path, sync)
		if err != nil {
			setFooter(fmt.Sprintf("%s fetch failed: %s", r.Name, gitx.FirstLine(out)))
			setRowStatus(r.Path, rowStatusFor(r.Path), "Fetch failed · "+elapsed, widget.DangerImportance)
		} else {
			setFooter(fmt.Sprintf("%s fetched in %s", r.Name, elapsed))
			setRowStatus(r.Path, rowStatusFor(r.Path), "Fetched · "+elapsed, widget.SuccessImportance)
		}
		fyne.Do(func() { reloadDetailPanel(r) })
	}()
}

func detailPull() {
	r, ok := activeRepo()
	if !ok {
		return
	}
	pullRepo(r, rowStatusFor(r.Path))
}

func detailPush() {
	r, ok := activeRepo()
	if !ok {
		return
	}
	showPushDialog([]Repo{r}, r.Name)
}

func detailStage(stage bool) {
	r, ok := activeRepo()
	if !ok {
		return
	}
	paths := selectedDetailPaths()
	go func() {
		var out string
		var err error
		if stage {
			out, err = gitx.Add(r.Path, paths...)
		} else {
			out, err = gitx.Unstage(r.Path, paths...)
		}
		if err != nil {
			setFooter(fmt.Sprintf("%s: %s", r.Name, gitx.FirstLine(out)))
		} else {
			action := "Staged"
			if !stage {
				action = "Unstaged"
			}
			setFooter(fmt.Sprintf("%s: %s", r.Name, action))
		}
		fyne.Do(func() { reloadDetailPanel(r) })
	}()
}

func detailDiscard() {
	r, ok := activeRepo()
	if !ok {
		return
	}
	paths := selectedDetailPaths()
	if len(paths) == 0 {
		setFooter("Select files to discard")
		return
	}

	dialog.ShowConfirm(
		"Discard changes",
		fmt.Sprintf("Discard local changes to %d file(s)? This cannot be undone.", len(paths)),
		func(ok bool) {
			if !ok {
				return
			}
			go func() {
				out, err := gitx.Discard(r.Path, paths...)
				if err != nil {
					setFooter(fmt.Sprintf("%s: %s", r.Name, gitx.FirstLine(out)))
				} else {
					setFooter(fmt.Sprintf("%s: discarded", r.Name))
				}
				fyne.Do(func() { reloadDetailPanel(r) })
			}()
		},
		appWindow,
	)
}

func detailStash() {
	r, ok := activeRepo()
	if !ok {
		return
	}
	msg := ""
	if detailStashMsg != nil {
		msg = detailStashMsg.Text
	}
	go func() {
		out, err := gitx.Stash(r.Path, msg)
		if err != nil {
			kind := gitx.Classify(gitx.OpStash, out)
			if kind == gitx.KindStashEmpty {
				setFooter(fmt.Sprintf("%s: no local changes to stash", r.Name))
			} else {
				setFooter(fmt.Sprintf("%s: %s", r.Name, gitx.FirstLine(out)))
			}
		} else {
			setFooter(fmt.Sprintf("%s: stashed", r.Name))
			fyne.Do(func() {
				if detailStashMsg != nil {
					detailStashMsg.SetText("")
				}
			})
		}
		updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
		fyne.Do(func() { reloadDetailPanel(r) })
	}()
}

func detailCommit() {
	r, ok := activeRepo()
	if !ok {
		return
	}
	msg := strings.TrimSpace(detailCommitMsg.Text)
	if msg == "" {
		setFooter("Commit message required")
		return
	}

	go func() {
		start := time.Now()
		var out string
		var err error
		if detailStageAll != nil && detailStageAll.Checked {
			out, err = gitx.CommitAll(r.Path, msg)
		} else {
			out, err = gitx.Commit(r.Path, msg)
		}
		elapsed := formatDuration(time.Since(start))
		status := rowStatusFor(r.Path)
		if err != nil {
			kind := handleClassifiedFailure(r, gitx.OpCommit, trimBody(out), status, elapsed, "", false)
			if kind == gitx.KindCommitEmpty {
				fyne.Do(func() {
					showConflictDialog("Nothing to commit", r.Name+"\n\n"+out, []conflictAction{
						{
							Label:   "Stage all & commit",
							Primary: true,
							Run: func() {
								go func() {
									o, e := gitx.CommitAll(r.Path, msg)
									el := formatDuration(time.Since(start))
									if e != nil {
										handleClassifiedFailure(r, gitx.OpCommit, trimBody(o), status, el, "", true)
										return
									}
									clearAttention(r.Path)
									updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
									setRowStatus(r.Path, status, "Committed in "+el, widget.SuccessImportance)
									setFooter(fmt.Sprintf("%s: committed", r.Name))
									fyne.Do(func() {
										detailCommitMsg.SetText("")
										reloadDetailPanel(r)
									})
								}()
							},
						},
					})
				})
			} else if kind.Actionable() {
				fyne.Do(func() {
					showAttentionDialog(attentionItem{Repo: r, Kind: kind, Out: trimBody(out), Op: gitx.OpCommit}, status)
				})
			}
			return
		}
		clearAttention(r.Path)
		setFooter(fmt.Sprintf("%s: committed", r.Name))
		setRowStatus(r.Path, status, "Committed in "+elapsed, widget.SuccessImportance)
		fyne.Do(func() {
			detailCommitMsg.SetText("")
		})
		updateRepoSync(r.Path, gitx.UpstreamSync(r.Path))
		fyne.Do(func() { reloadDetailPanel(r) })
	}()
}
