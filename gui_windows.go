//go:build windows

// Native Windows window for GK2 Item Spawner (lxn/walk).
package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type spawnerUI struct {
	mw        *walk.MainWindow
	statusDot *walk.Label
	status    *walk.Label
	search    *walk.LineEdit
	list      *walk.ListBox
	count     *walk.NumberEdit
	addBtn    *walk.PushButton
	logBox    *walk.TextEdit
	countInfo *walk.Label

	items    []Item
	shown    []Item // filtered view backing the list box
	model    *labelModel
	query    string // search text the list currently shows
	ready    atomic.Bool
	busy     atomic.Bool
	lastWarn string
}

func main() {
	items, err := loadItems()
	if err != nil {
		walk.MsgBox(nil, "GK2 Item Spawner", "Cannot read the item list: "+err.Error(), walk.MsgBoxIconError)
		return
	}
	ui := &spawnerUI{items: items}
	if err := ui.run(); err != nil {
		walk.MsgBox(nil, "GK2 Item Spawner", "Cannot open the window: "+err.Error(), walk.MsgBoxIconError)
	}
}

func (ui *spawnerUI) run() error {
	bold := Font{Bold: true}
	err := MainWindow{
		AssignTo: &ui.mw,
		Title:    "GK2 Item Spawner " + appVersion,
		MinSize:  Size{Width: 600, Height: 960},
		Size:     Size{Width: 660, Height: 1040},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{AssignTo: &ui.statusDot, Text: "●", TextColor: walk.RGB(200, 70, 60), Font: Font{PointSize: 14}},
					Label{AssignTo: &ui.status, Text: "Looking for the game…", Font: bold},
					HSpacer{},
					PushButton{Text: "Reconnect", OnClicked: func() { go ui.tryConnect(true) }},
				},
			},
			GroupBox{
				Title:  "Items",
				Layout: VBox{},
				Children: []Widget{
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							LineEdit{
								AssignTo:      &ui.search,
								CueBanner:     "Search items, e.g. heart, iron, faith…",
								OnTextChanged: ui.filter,
								OnKeyUp: func(key walk.Key) {
									if key == walk.KeyReturn {
										ui.addSelected()
										return
									}
									ui.filter()
								},
							},
							PushButton{Text: "✕", MaxSize: Size{Width: 32}, ToolTipText: "Clear search", OnClicked: func() {
								ui.search.SetText("")
								ui.filter()
								ui.search.SetFocus()
							}},
						},
					},
					Label{AssignTo: &ui.countInfo},
					ListBox{AssignTo: &ui.list, MinSize: Size{Height: 260}, OnItemActivated: ui.addSelected, OnCurrentIndexChanged: ui.updateButtons},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "Count:"},
							NumberEdit{AssignTo: &ui.count, Value: 1.0, MinValue: 1, MaxValue: maxCount, Decimals: 0, MaxSize: Size{Width: 90}},
							PushButton{Text: "×1", MaxSize: Size{Width: 40}, OnClicked: func() { ui.count.SetValue(1) }},
							PushButton{Text: "×10", MaxSize: Size{Width: 45}, OnClicked: func() { ui.count.SetValue(10) }},
							PushButton{Text: "×50", MaxSize: Size{Width: 45}, OnClicked: func() { ui.count.SetValue(50) }},
							PushButton{Text: "Stack", MaxSize: Size{Width: 55}, OnClicked: ui.setStack},
							HSpacer{},
							PushButton{AssignTo: &ui.addBtn, Text: "Add to inventory", Font: bold, OnClicked: ui.addSelected},
						},
					},
				},
			},
			Label{Text: "Log"},
			TextEdit{AssignTo: &ui.logBox, ReadOnly: true, VScroll: true, MinSize: Size{Height: 110}},
		},
	}.Create()
	if err != nil {
		return err
	}
	ui.filter()
	ui.updateButtons()
	ui.log("Start the game and load your save - the spawner connects automatically.")
	go ui.watchGame()
	go ui.watchSearch()
	ui.search.SetFocus()
	ui.mw.Run()
	return nil
}

// labelModel is the list box's data source; PublishItemsReset makes the list redraw.
type labelModel struct {
	walk.ListModelBase
	labels []string
}

func (m *labelModel) ItemCount() int              { return len(m.labels) }
func (m *labelModel) Value(index int) interface{} { return m.labels[index] }

// filter rebuilds the list from the search box. Safe to call often: it only
// does work when the search text actually changed (or on the first call).
func (ui *spawnerUI) filter() {
	if ui.search == nil || ui.list == nil || ui.countInfo == nil {
		return // still creating the window
	}
	q := strings.ToLower(strings.TrimSpace(ui.search.Text()))
	if ui.model != nil && q == ui.query {
		return
	}
	ui.query = q

	prevID := ""
	if it := ui.selected(); it != nil {
		prevID = it.ID // copy before ui.shown is rebuilt
	}
	shown := make([]Item, 0, len(ui.items))
	labels := make([]string, 0, len(ui.items))
	words := strings.Fields(q) // every word must match: "heart gold", "iron kit"
	sel := -1
	for _, it := range ui.items {
		hay := strings.ToLower(it.Name + " " + it.ID + " " + it.Detail)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if it.ID == prevID {
			sel = len(shown)
		}
		shown = append(shown, it)
		labels = append(labels, it.Label())
	}
	ui.shown = shown
	if ui.model == nil {
		ui.model = &labelModel{labels: labels}
		ui.list.SetModel(ui.model)
	} else {
		ui.model.labels = labels
		ui.model.PublishItemsReset()
	}
	if sel < 0 && len(shown) > 0 {
		sel = 0
	}
	ui.list.SetCurrentIndex(sel)
	ui.list.Invalidate()
	ui.countInfo.SetText(fmt.Sprintf("%d of %d items", len(shown), len(ui.items)))
	ui.updateButtons()
}

// watchSearch re-checks the search box a few times a second, so the list
// follows the text even if a key/text event is missed.
func (ui *spawnerUI) watchSearch() {
	for range time.Tick(250 * time.Millisecond) {
		ui.mw.Synchronize(ui.filter)
	}
}

func (ui *spawnerUI) selected() *Item {
	if ui.list == nil {
		return nil
	}
	i := ui.list.CurrentIndex()
	if i < 0 || i >= len(ui.shown) {
		return nil
	}
	return &ui.shown[i]
}

func (ui *spawnerUI) setStack() {
	if it := ui.selected(); it != nil && it.Stack > 0 {
		ui.count.SetValue(float64(it.Stack))
	}
}

func (ui *spawnerUI) updateButtons() {
	if ui.addBtn == nil {
		return
	}
	on := ui.ready.Load() && !ui.busy.Load()
	ui.addBtn.SetEnabled(on && ui.selected() != nil)
}

func (ui *spawnerUI) addSelected() {
	it := ui.selected()
	if it == nil || !ui.ready.Load() {
		return
	}
	id, n := it.ID, int(ui.count.Value())
	ui.sendAsync(func() (bool, string) { return addItem(id, n) })
}

// sendAsync runs a game request off the UI thread and logs the answer.
func (ui *spawnerUI) sendAsync(req func() (bool, string)) {
	if !ui.busy.CompareAndSwap(false, true) {
		return
	}
	ui.updateButtons()
	go func() {
		ok, msg := req()
		ui.mw.Synchronize(func() {
			ui.busy.Store(false)
			if ok {
				ui.log("✔ " + msg)
			} else {
				ui.log("✖ " + msg)
			}
			ui.updateButtons()
		})
	}()
}

func (ui *spawnerUI) log(msg string) {
	line := time.Now().Format("15:04:05") + "  " + msg + "\r\n"
	ui.logBox.SetText(line + ui.logBox.Text()) // newest on top
}

// watchGame keeps the status up to date and injects the helper automatically.

func (ui *spawnerUI) watchGame() {
	for {
		ui.tryConnect(false)
		time.Sleep(3 * time.Second)
	}
}

var connecting atomic.Bool

func (ui *spawnerUI) tryConnect(manual bool) {
	if !connecting.CompareAndSwap(false, true) {
		return
	}
	defer connecting.Store(false)

	var text, warn string
	var ready bool
	var color walk.Color = walk.RGB(200, 70, 60)
	switch st := probeHelper(); {
	case st == helperReady:
		ready, text, color = true, "Connected to Graveyard Keeper 2 · helper v1", walk.RGB(70, 160, 70)
	case st == helperOutdated:
		text, warn, color = "Old helper in the game - restart the game", outdatedMsg, walk.RGB(210, 160, 40)
	case !gameRunning():
		text = "Waiting for the game to start…"
	default:
		err := connectToGame()
		if err == nil {
			ready, text, color = true, "Connected to Graveyard Keeper 2 · helper v1", walk.RGB(70, 160, 70)
			ui.mw.Synchronize(func() { ui.log("Connected - helper loaded into the game.") })
		} else {
			// Mono isn't ready until the main menu; keep retrying quietly.
			text, warn = "Game found - connecting…", err.Error()
		}
	}
	ui.mw.Synchronize(func() {
		wasReady := ui.ready.Swap(ready)
		ui.status.SetText(text)
		ui.statusDot.SetTextColor(color)
		if wasReady && !ready {
			ui.log("Lost connection to the game.")
		}
		if warn != "" && (manual || warn != ui.lastWarn) {
			ui.log("… " + warn)
		}
		ui.lastWarn = warn
		ui.updateButtons()
	})
}
