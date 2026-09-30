//go:build windows

// Native Windows window for GK2 Item Spawner (lxn/walk).
package main

import (
	"fmt"
	"os"
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
	techR     *walk.NumberEdit
	techG     *walk.NumberEdit
	techB     *walk.NumberEdit
	techBtn   *walk.PushButton
	techRead  *walk.PushButton
	moneyG    *walk.NumberEdit
	moneyS    *walk.NumberEdit
	moneyC    *walk.NumberEdit
	moneyAdd  *walk.PushButton
	moneyDel  *walk.PushButton
	moneyRead *walk.PushButton
	instant   *walk.CheckBox
	zombieLbl *walk.Label
	zombieBtn *walk.PushButton
	zombieOn  atomic.Bool // a zombie's menu is open in the game
	corpseLbl *walk.Label
	corpseBtn *walk.PushButton
	corpseOn  atomic.Bool // a dead body is on an open table / grave, or carried
	timeLbl   *walk.Label
	ffTarget  *walk.ComboBox
	ffHour    *walk.NumberEdit
	ffSpeed   *walk.ComboBox
	ffRested  *walk.CheckBox
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
	// The modern trainer window needs the WebView2 runtime (built into Windows 10/11);
	// without it, fall back to the classic native window.
	// Start with --classic for the fast plain window.
	classic := false
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, "--classic") || strings.EqualFold(arg, "/classic") {
			classic = true
		}
	}
	if !classic && runWebUI(items) {
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
			GroupBox{
				Title:  "Tech points (max 999 each)",
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Red", TextColor: walk.RGB(180, 60, 45)},
					NumberEdit{AssignTo: &ui.techR, MinValue: 0, MaxValue: techCap, Decimals: 0, MaxSize: Size{Width: 70}},
					Label{Text: "Green", TextColor: walk.RGB(70, 140, 60)},
					NumberEdit{AssignTo: &ui.techG, MinValue: 0, MaxValue: techCap, Decimals: 0, MaxSize: Size{Width: 70}},
					Label{Text: "Blue", TextColor: walk.RGB(60, 100, 180)},
					NumberEdit{AssignTo: &ui.techB, MinValue: 0, MaxValue: techCap, Decimals: 0, MaxSize: Size{Width: 70}},
					HSpacer{},
					PushButton{AssignTo: &ui.techBtn, Text: "Add", OnClicked: ui.addTechPoints},
					PushButton{AssignTo: &ui.techRead, Text: "Show", OnClicked: func() { ui.sendAsync(func() (bool, string) { return addTech(0, 0, 0) }) }},
				},
			},
			GroupBox{
				Title:  "Money (1 gold = 100 silver = 10000 copper)",
				Layout: HBox{},
				Children: []Widget{
					Label{Text: "Gold", TextColor: walk.RGB(190, 150, 30)},
					NumberEdit{AssignTo: &ui.moneyG, MinValue: 0, MaxValue: moneyMax / 10000, Decimals: 0, MaxSize: Size{Width: 80}},
					Label{Text: "Silver", TextColor: walk.RGB(120, 120, 130)},
					NumberEdit{AssignTo: &ui.moneyS, MinValue: 0, MaxValue: 99, Decimals: 0, MaxSize: Size{Width: 55}},
					Label{Text: "Copper", TextColor: walk.RGB(170, 100, 50)},
					NumberEdit{AssignTo: &ui.moneyC, MinValue: 0, MaxValue: 99, Decimals: 0, MaxSize: Size{Width: 55}},
					HSpacer{},
					PushButton{AssignTo: &ui.moneyAdd, Text: "Add", OnClicked: func() { ui.changeMoney(1) }},
					PushButton{AssignTo: &ui.moneyDel, Text: "Remove", OnClicked: func() { ui.changeMoney(-1) }},
					PushButton{AssignTo: &ui.moneyRead, Text: "Show", OnClicked: func() { ui.sendAsync(func() (bool, string) { return changeMoney(0) }) }},
				},
			},
			GroupBox{
				Title:  "Crafting",
				Layout: HBox{},
				Children: []Widget{
					CheckBox{
						AssignTo:         &ui.instant,
						Text:             "Instant craft - one hit finishes what you are crafting",
						OnCheckedChanged: func() { ui.syncInstant(true) },
					},
					HSpacer{},
				},
			},
			GroupBox{
				Title:  "Zombie",
				Layout: HBox{},
				Children: []Widget{
					Label{AssignTo: &ui.zombieLbl, Text: "Open a zombie's menu in the game to edit it."},
					HSpacer{},
					PushButton{AssignTo: &ui.zombieBtn, Text: "Edit zombie…", Font: Font{Bold: true}, OnClicked: ui.openZombieEditor},
				},
			},
			GroupBox{
				Title:  "Dead body",
				Layout: HBox{},
				Children: []Widget{
					Label{AssignTo: &ui.corpseLbl, Text: corpseIdleText},
					HSpacer{},
					PushButton{AssignTo: &ui.corpseBtn, Text: "Edit body…", Font: Font{Bold: true}, OnClicked: ui.openCorpseEditor},
				},
			},
			GroupBox{
				Title:  "Time - fast-forward (the world runs faster, nothing is skipped)",
				Layout: VBox{},
				Children: []Widget{
					Label{AssignTo: &ui.timeLbl, Text: "Time: -", Font: Font{Bold: true}},
					Composite{
						Layout: HBox{MarginsZero: true},
						Children: []Widget{
							Label{Text: "Until:"},
							ComboBox{AssignTo: &ui.ffTarget, Model: ffTargetLabels, CurrentIndex: 0, MinSize: Size{Width: 130}},
							Label{Text: "at hour"},
							NumberEdit{AssignTo: &ui.ffHour, Value: 6.0, MinValue: 0, MaxValue: 23, Decimals: 0, MaxSize: Size{Width: 45}},
							Label{Text: "speed"},
							ComboBox{AssignTo: &ui.ffSpeed, Model: []string{"x10", "x25", "x50 (sleep speed)"}, CurrentIndex: 1, MaxSize: Size{Width: 120}},
							CheckBox{AssignTo: &ui.ffRested, Text: "Stay rested", Checked: true, ToolTipText: "Clear the lack-of-sleep debuff each game day while fast-forwarding"},
							HSpacer{},
							PushButton{Text: "Fast-forward", Font: Font{Bold: true}, OnClicked: ui.startFastForward},
							PushButton{Text: "Stop", OnClicked: func() { ui.sendAsync(timeStop) }},
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
	go ui.watchZombie()
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
	ui.techBtn.SetEnabled(on)
	ui.techRead.SetEnabled(on)
	ui.moneyAdd.SetEnabled(on)
	ui.moneyDel.SetEnabled(on)
	ui.moneyRead.SetEnabled(on)
	ui.zombieBtn.SetEnabled(ui.ready.Load() && ui.zombieOn.Load())
	ui.corpseBtn.SetEnabled(ui.ready.Load() && ui.corpseOn.Load())
}

func (ui *spawnerUI) addSelected() {
	it := ui.selected()
	if it == nil || !ui.ready.Load() {
		return
	}
	id, n := it.ID, int(ui.count.Value())
	ui.sendAsync(func() (bool, string) { return addItem(id, n) })
}

func (ui *spawnerUI) addTechPoints() {
	r, g, b := int(ui.techR.Value()), int(ui.techG.Value()), int(ui.techB.Value())
	if r+g+b == 0 {
		ui.log("Enter how many red / green / blue points to add.")
		return
	}
	ui.sendAsync(func() (bool, string) { return addTech(r, g, b) })
}

// changeMoney adds (sign 1) or removes (sign -1) the gold/silver/copper amount entered.
func (ui *spawnerUI) changeMoney(sign int) {
	copper := int(ui.moneyG.Value())*10000 + int(ui.moneyS.Value())*100 + int(ui.moneyC.Value())
	if copper == 0 {
		ui.log("Enter an amount of gold / silver / copper first.")
		return
	}
	ui.sendAsync(func() (bool, string) { return changeMoney(sign * copper) })
}

// syncInstant sends the Instant craft checkbox to the game. It also runs on every
// (re)connect, because a restarted game starts with the option off.
func (ui *spawnerUI) syncInstant(logIt bool) {
	on := ui.instant.Checked()
	if !ui.ready.Load() {
		if logIt {
			ui.log("Instant craft will be applied when the spawner connects to the game.")
		}
		return
	}
	go func() {
		ok, msg := setInstantCraft(on)
		if logIt || !ok {
			ui.mw.Synchronize(func() {
				if ok {
					ui.log("✔ " + msg)
				} else {
					ui.log("✖ " + msg)
				}
			})
		}
	}()
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
const corpseIdleText = "Open an autopsy / embalming table or grave with a body, or carry a body."

var corpsePlaceText = map[string]string{
	"autopsy": "on the autopsy table",
	"embalm":  "on the embalming table",
	"grave":   "in the grave",
	"carried": "carried",
}

var ffTargetLabels = []string{"Next day", "In 2 days", "In 3 days", "Gluttony (1)", "Sloth (2)", "Lust (3)", "Envy (4)", "Pride (5)", "Wrath (6)"}
var ffTargetArgs = []string{"+1", "+2", "+3", "1", "2", "3", "4", "5", "6"}
var ffSpeeds = []int{10, 25, 50}

func (ui *spawnerUI) startFastForward() {
	i, j := ui.ffTarget.CurrentIndex(), ui.ffSpeed.CurrentIndex()
	if i < 0 || j < 0 {
		return
	}
	target, hour, speed, rested := ffTargetArgs[i], int(ui.ffHour.Value()), ffSpeeds[j], ui.ffRested.Checked()
	ui.sendAsync(func() (bool, string) { return timeFastForward(target, hour, speed, rested) })
}

// timeText is the Time section's status line.
func timeText(t *GameTime) string {
	name := ""
	if t.Weekday >= 1 && t.Weekday <= 6 {
		name = weekdayNames[t.Weekday]
	}
	s := fmt.Sprintf("Day %d · %s · %s   (a game day lasts %.0f real min)", t.Day, name, t.Clock(), t.DayMinutes)
	if t.FFTarget != "" && t.FFTarget != "-" {
		var day int
		var tod float64
		fmt.Sscanf(strings.Replace(t.FFTarget, "@", " ", 1), "%d %f", &day, &tod)
		s += fmt.Sprintf("   ⏩ x%.0f until day %d, %s", t.Speed, day, GameTime{TimeOfDay: tod}.Clock())
	} else if t.Speed > 1.01 {
		s += fmt.Sprintf("   (world speed x%.0f - sleeping?)", t.Speed)
	}
	return s
}

// watchZombie checks once a second whether a zombie's menu is open in the game, and whether a
// dead body can be edited, and enables the matching "Edit…" buttons.
func (ui *spawnerUI) watchZombie() {
	lastZombie, lastCorpse := "\x00", "\x00"
	for range time.Tick(time.Second) {
		zOpen, zName, cOpen, cDesc := false, "", false, ""
		if ui.ready.Load() {
			zOpen, zName = zombieOpen()
			cOpen, cDesc = corpseOpen()
		}
		if ui.ready.Load() {
			if t, err := timeGet(); err == nil {
				txt := timeText(t)
				ui.mw.Synchronize(func() { ui.timeLbl.SetText(txt) })
			}
		}
		zKey, cKey := fmt.Sprint(zOpen, zName), fmt.Sprint(cOpen, cDesc)
		if zKey == lastZombie && cKey == lastCorpse {
			continue
		}
		lastZombie, lastCorpse = zKey, cKey
		ui.zombieOn.Store(zOpen)
		ui.corpseOn.Store(cOpen)
		ui.mw.Synchronize(func() {
			if zOpen {
				ui.zombieLbl.SetText("Zombie menu open in game: " + zName)
			} else {
				ui.zombieLbl.SetText("Open a zombie's menu in the game to edit it.")
			}
			if cOpen {
				where, name, _ := strings.Cut(cDesc, " ")
				ui.corpseLbl.SetText(fmt.Sprintf("Body %s: %s", corpsePlaceText[where], name))
			} else {
				ui.corpseLbl.SetText(corpseIdleText)
			}
			ui.updateButtons()
		})
	}
}

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
		ready, text, color = true, "Connected to Graveyard Keeper 2 · helper v7", walk.RGB(70, 160, 70)
	case st == helperOutdated:
		text, warn, color = "Old helper in the game - restart the game", outdatedMsg, walk.RGB(210, 160, 40)
	case !gameRunning():
		text = "Waiting for the game to start…"
	default:
		err := connectToGame()
		if err == nil {
			ready, text, color = true, "Connected to Graveyard Keeper 2 · helper v7", walk.RGB(70, 160, 70)
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
		if ready && !wasReady && ui.instant.Checked() {
			ui.syncInstant(true)
		}
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
