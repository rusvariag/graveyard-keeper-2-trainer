//go:build windows

// Zombie editor window: edits the zombie whose menu is open in the game.
package main

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type zombieEditor struct {
	ui  *spawnerUI
	dlg *walk.Dialog

	nameRow                        *walk.Composite
	pointsBox, equipBox, skillsBox *walk.GroupBox

	title, skulls, warn, status *walk.Label
	name                        *walk.LineEdit
	techR, techG, techB         *walk.NumberEdit
	collar, hand, armor         *walk.ComboBox
	body                        *walk.ListBox
	addItem                     *walk.ComboBox
	replaceList                 *walk.ListBox
	replaceHint                 *walk.Label
	replaceBtn                  *walk.PushButton
	replaceModel                *labelModel
	addCount                    *walk.NumberEdit
	branch                      *walk.ComboBox
	skills                      *walk.ListBox

	bodyModel, skillModel             *labelModel
	info                              *ZombieInfo
	cat                               *ZombieCatalog
	collarIDs, handIDs, armorIDs      []string // "" = empty slot
	replaceIDs, addIDs, shownSkillIDs []string
	selectUID, selectID               string   // body item to re-select after a reload
	pendingSelectID                   string   // set by Replace: select the new organ afterwards
	branchIDs                         []string // "" = all branches
	busy                              atomic.Bool
}

// openZombieEditor loads the open zombie and shows the editor (runs on the UI thread).
func (ui *spawnerUI) openZombieEditor() {
	ed := &zombieEditor{ui: ui}
	cat, err := zombieCatalog() // organs and embalming items; works without an open zombie menu
	if err == nil {
		ed.cat = cat
		if ed.info, err = ed.get(); err == nil {
			if err = ed.run(); err == nil {
				return
			}
		}
	}
	ui.log("✖ zombie editor: " + err.Error())
}

// send runs one ZOMBIE sub-command.
func (ed *zombieEditor) send(cmd string) (bool, string) {
	return zombieCmd(cmd)
}

func (ed *zombieEditor) get() (*ZombieInfo, error) {
	return zombieGet()
}

func (ed *zombieEditor) itemName(id string) string {
	for _, it := range ed.ui.items {
		if it.ID == id {
			if it.Name != it.ID {
				return fmt.Sprintf("%s (%s)", it.Name, id)
			}
			break
		}
	}
	return id
}

func (ed *zombieEditor) run() error {
	bold := Font{Bold: true}
	ed.bodyModel = &labelModel{}
	ed.skillModel = &labelModel{}
	ed.replaceModel = &labelModel{}
	err := Dialog{
		AssignTo: &ed.dlg,
		Title:    "Zombie editor",
		MinSize:  Size{Width: 720, Height: 760},
		Layout:   VBox{},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{AssignTo: &ed.title, Font: Font{Bold: true, PointSize: 11}},
				HSpacer{},
				PushButton{Text: "Reload from game", OnClicked: func() { ed.do("") }},
			}},
			Label{AssignTo: &ed.skulls, Font: bold},
			Label{AssignTo: &ed.warn, TextColor: walk.RGB(190, 60, 40)},
			Composite{AssignTo: &ed.nameRow, Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "Name:"},
				LineEdit{AssignTo: &ed.name, MaxLength: 40},
				PushButton{Text: "Rename", OnClicked: func() { ed.do("NAME " + ed.name.Text()) }},
			}},
			GroupBox{AssignTo: &ed.pointsBox, Title: "Points to spend on the skill tree", Layout: HBox{}, Children: []Widget{
				Label{Text: "Red", TextColor: walk.RGB(180, 60, 45)},
				NumberEdit{AssignTo: &ed.techR, MaxValue: 99999, Decimals: 0, MaxSize: Size{Width: 80}},
				Label{Text: "Green", TextColor: walk.RGB(70, 140, 60)},
				NumberEdit{AssignTo: &ed.techG, MaxValue: 99999, Decimals: 0, MaxSize: Size{Width: 80}},
				Label{Text: "Blue", TextColor: walk.RGB(60, 100, 180)},
				NumberEdit{AssignTo: &ed.techB, MaxValue: 99999, Decimals: 0, MaxSize: Size{Width: 80}},
				HSpacer{},
				PushButton{Text: "Set points", OnClicked: func() {
					ed.do(fmt.Sprintf("TECH %d %d %d", int(ed.techR.Value()), int(ed.techG.Value()), int(ed.techB.Value())))
				}},
			}},
			GroupBox{AssignTo: &ed.equipBox, Title: "Equipment", Layout: Grid{Columns: 4}, Children: []Widget{
				Label{Text: "Collar (belt):"},
				ComboBox{AssignTo: &ed.collar, ColumnSpan: 2, MinSize: Size{Width: 320}},
				PushButton{Text: "Set collar", OnClicked: func() { ed.equip("collar", ed.collar, ed.collarIDs) }},
				Label{Text: "Tool / weapon:"},
				ComboBox{AssignTo: &ed.hand, ColumnSpan: 2, MinSize: Size{Width: 320}},
				PushButton{Text: "Set tool", OnClicked: func() { ed.equip("hand", ed.hand, ed.handIDs) }},
				Label{Text: "Armour:"},
				ComboBox{AssignTo: &ed.armor, ColumnSpan: 2, MinSize: Size{Width: 320}},
				PushButton{Text: "Set armour", OnClicked: func() { ed.equip("armor", ed.armor, ed.armorIDs) }},
			}},
			GroupBox{Title: "Body - organs and items (skulls come from these)", Layout: VBox{}, Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					// left: what is in the body; right: what the selected item can be swapped for
					Composite{Layout: VBox{MarginsZero: true}, StretchFactor: 3, Children: []Widget{
						Label{Text: "In the body - click one:"},
						ListBox{AssignTo: &ed.body, MinSize: Size{Width: 300, Height: 150}, OnCurrentIndexChanged: ed.fillReplace},
						PushButton{Text: "Remove selected", OnClicked: ed.removeSelected},
					}},
					Composite{Layout: VBox{MarginsZero: true}, StretchFactor: 2, Children: []Widget{
						Label{AssignTo: &ed.replaceHint, Text: "Replace with:"},
						ListBox{AssignTo: &ed.replaceList, MinSize: Size{Width: 240, Height: 150}, OnItemActivated: ed.replaceSelected},
						PushButton{AssignTo: &ed.replaceBtn, Text: "Replace with selected", OnClicked: ed.replaceSelected},
					}},
				}},
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					Label{Text: "Add:"},
					ComboBox{AssignTo: &ed.addItem, MinSize: Size{Width: 320}},
					NumberEdit{AssignTo: &ed.addCount, Value: 1.0, MinValue: 1, MaxValue: 99, Decimals: 0, MaxSize: Size{Width: 50}},
					PushButton{Text: "Add to body", OnClicked: func() {
						if i := ed.addItem.CurrentIndex(); i >= 0 && i < len(ed.addIDs) {
							ed.do(fmt.Sprintf("ADD %s %d", ed.addIDs[i], int(ed.addCount.Value())))
						}
					}},
				}},
			}},
			GroupBox{AssignTo: &ed.skillsBox, Title: "Skill tree", Layout: VBox{}, Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					Label{Text: "Branch:"},
					ComboBox{AssignTo: &ed.branch, OnCurrentIndexChanged: ed.fillSkills},
					HSpacer{},
					PushButton{Text: "Learn", OnClicked: func() { ed.skillSelected(true) }},
					PushButton{Text: "Forget", OnClicked: func() { ed.skillSelected(false) }},
					PushButton{Text: "Learn all shown", OnClicked: func() { ed.skillAll(true) }},
					PushButton{Text: "Forget all shown", OnClicked: func() { ed.skillAll(false) }},
				}},
				ListBox{AssignTo: &ed.skills, MinSize: Size{Height: 150}, OnItemActivated: func() { ed.skillToggle() }},
				Label{Text: "[x] learned   [~] learned but inactive (needs more red skulls)   [ ] not learned.  Double-click toggles."},
			}},
			Label{AssignTo: &ed.status},
		},
	}.Create(ed.ui.mw)
	if err != nil {
		return err
	}
	ed.body.SetModel(ed.bodyModel)
	ed.skills.SetModel(ed.skillModel)
	ed.replaceList.SetModel(ed.replaceModel)
	ed.fillStatic()
	ed.fillFromInfo()
	ed.dlg.Run()
	return nil
}

// fillStatic fills the choices that come from the catalog (they don't change while editing).
func (ed *zombieEditor) fillStatic() {
	limits := map[string]int{}
	for _, c := range ed.cat.CollarLimits {
		limits[c.ID] = c.RedMax
	}
	var labels []string
	ed.collarIDs = append([]string(nil), ed.cat.Collars...)
	sort.Slice(ed.collarIDs, func(i, j int) bool { return limits[ed.collarIDs[i]] < limits[ed.collarIDs[j]] })
	for _, id := range ed.collarIDs {
		labels = append(labels, fmt.Sprintf("%s - up to %d red skulls", ed.itemName(id), limits[id]))
	}
	ed.collar.SetModel(labels)

	ed.handIDs, labels = equipChoices(ed, ed.cat.Hands)
	ed.hand.SetModel(labels)
	ed.armorIDs, labels = equipChoices(ed, ed.cat.Armors)
	ed.armor.SetModel(labels)

	body := append(ed.cat.Body[:0:0], ed.cat.Body...)
	sort.Slice(body, func(i, j int) bool {
		if body[i].Group != body[j].Group {
			return body[i].Group < body[j].Group
		}
		return body[i].ID < body[j].ID
	})
	ed.addIDs, labels = nil, nil
	for _, b := range body {
		ed.addIDs = append(ed.addIDs, b.ID)
		labels = append(labels, fmt.Sprintf("%s  W%+d R%+d", ed.itemName(b.ID), b.White, b.Red))
	}
	ed.addItem.SetModel(labels)

	ed.branchIDs, labels = []string{""}, []string{"All branches"}
	for _, b := range ed.cat.Branches {
		ed.branchIDs = append(ed.branchIDs, b.ID)
		labels = append(labels, branchLabel(b.ID, b.Name))
	}
	ed.branch.SetModel(labels)
	ed.branch.SetCurrentIndex(0)
}

func equipChoices(ed *zombieEditor, ids []string) ([]string, []string) {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	outIDs, labels := []string{""}, []string{"(empty)"}
	for _, id := range sorted {
		outIDs = append(outIDs, id)
		labels = append(labels, ed.itemName(id))
	}
	return outIDs, labels
}

func branchLabel(id, name string) string {
	if name != "" {
		return name
	}
	s := strings.TrimPrefix(id, "talent_")
	return strings.ToUpper(s[:1]) + s[1:] + " branch"
}

// fillFromInfo shows the zombie's current state.
func (ed *zombieEditor) fillFromInfo() {
	z := ed.info
	ed.title.SetText(fmt.Sprintf("%s  -  %s", z.Name, z.Type))
	active := z.PerksUsed - z.PerksDisabled
	ed.skulls.SetText(fmt.Sprintf("White skulls: %d     Red skulls: %d  (skill slots: %d active of %d learned)", z.White, z.Red, active, z.PerksUsed))
	var warn []string
	if !z.InCollarLimits {
		warn = append(warn, fmt.Sprintf("Red skulls are outside this collar's limit (max %d) - the game may block body changes until you fix it.", z.CollarRedMax))
	}
	if z.PerksDisabled > 0 {
		warn = append(warn, fmt.Sprintf("%d learned skill(s) are inactive: add red skulls (organs / embalming) to activate them.", z.PerksDisabled))
	}
	ed.warn.SetText(strings.Join(warn, "  "))
	if !ed.name.Focused() {
		ed.name.SetText(z.Name)
	}
	ed.techR.SetValue(float64(z.TechRed))
	ed.techG.SetValue(float64(z.TechGreen))
	ed.techB.SetValue(float64(z.TechBlue))
	for slot, cb := range map[string]*walk.ComboBox{"collar": ed.collar, "hand": ed.hand, "armor": ed.armor} {
		ids := map[string][]string{"collar": ed.collarIDs, "hand": ed.handIDs, "armor": ed.armorIDs}[slot]
		cur := ""
		if it := z.Equipped(slot); it != nil {
			cur = it.ID
		}
		cb.SetCurrentIndex(indexOf(ids, cur))
	}
	ed.fillBody()
	ed.fillSkills()
}

// fillBody shows the body's items and keeps the selection.
func (ed *zombieEditor) fillBody() {
	z := ed.info

	// keep the selection on the same item (or on the organ it was just swapped for)
	sel := -1
	for i, it := range z.Items {
		if (ed.selectID != "" && it.ID == ed.selectID && it.Slot == "") || (ed.selectID == "" && it.UID == ed.selectUID) {
			sel = i
			break
		}
	}
	ed.selectID, ed.selectUID = "", ""
	var labels []string
	for _, it := range z.Items {
		tag := ""
		if it.Slot != "" {
			tag = "   [equipped " + it.Slot + "]"
		} else if it.White != 0 || it.Red != 0 {
			tag = fmt.Sprintf("   W%+d R%+d", it.White, it.Red)
		}
		labels = append(labels, fmt.Sprintf("%s  x%d%s", ed.itemName(it.ID), it.Count, tag))
	}
	ed.bodyModel.labels = labels
	ed.bodyModel.PublishItemsReset()
	ed.body.SetCurrentIndex(sel)
	ed.fillReplace()
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

func (ed *zombieEditor) selectedBodyItem() *ZombieBodyItem {
	i := ed.body.CurrentIndex()
	if ed.info == nil || i < 0 || i >= len(ed.info.Items) {
		return nil
	}
	return &ed.info.Items[i]
}

// fillReplace lists what the selected body item can be swapped for: other kinds of the same
// organ (e.g. the 5 other hearts), or other skull items for liquids and injections.
func (ed *zombieEditor) fillReplace() {
	if ed.replaceList == nil || ed.replaceHint == nil || ed.replaceBtn == nil || ed.replaceModel == nil || ed.cat == nil {
		return // still creating the window
	}
	it := ed.selectedBodyItem()
	ed.replaceIDs = nil
	var labels []string
	hint := ""
	switch {
	case it == nil:
		hint = "Click an item on the left to see what it can be replaced with."
	case it.Slot != "":
		hint = "That's equipment - change it in the Equipment section above."
	default:
		type option struct {
			id         string
			white, red int
		}
		var opts []option
		for _, b := range ed.cat.Body {
			if b.Group == it.Group && b.ID != it.ID {
				opts = append(opts, option{b.ID, b.White, b.Red})
			}
		}
		// best first: most red skulls, then most white
		sort.Slice(opts, func(i, j int) bool {
			if opts[i].red != opts[j].red {
				return opts[i].red > opts[j].red
			}
			return opts[i].white > opts[j].white
		})
		for _, o := range opts {
			ed.replaceIDs = append(ed.replaceIDs, o.id)
			labels = append(labels, fmt.Sprintf("%s   W%+d R%+d", ed.itemName(o.id), o.white, o.red))
		}
		kind := strings.TrimSuffix(strings.TrimPrefix(it.Group, "gr_"), "s")
		switch {
		case len(opts) == 0 && it.Group != "":
			hint = fmt.Sprintf("There is only one kind of %s in the game - nothing to swap it for. Use Remove, or Add something else.", kind)
		case len(opts) == 0:
			hint = "Nothing to swap this for. Use Remove, or Add something else."
		case it.Group == "":
			hint = fmt.Sprintf("Replace with (%d skull items, best first):", len(opts))
		default:
			hint = fmt.Sprintf("Replace with another %s (%d kinds, best first):", kind, len(opts))
		}
	}
	ed.replaceHint.SetText(hint)
	ed.replaceModel.labels = labels
	ed.replaceModel.PublishItemsReset()
	if len(labels) > 0 {
		ed.replaceList.SetCurrentIndex(0)
	}
	ed.replaceBtn.SetEnabled(len(labels) > 0)
}

func (ed *zombieEditor) fillSkills() {
	if ed.skills == nil || ed.info == nil {
		return
	}
	branch := ""
	if i := ed.branch.CurrentIndex(); i > 0 && i < len(ed.branchIDs) {
		branch = ed.branchIDs[i]
	}
	names := map[string]string{}
	for _, b := range ed.cat.Branches {
		names[b.ID] = branchLabel(b.ID, b.Name)
	}
	sel := ed.skills.CurrentIndex()
	ed.shownSkillIDs = nil
	var labels []string
	for _, s := range ed.cat.Skills {
		if branch != "" && s.Branch != branch {
			continue
		}
		learned, inactive := ed.info.Learned(s.ID)
		mark := "[ ]"
		if learned && inactive {
			mark = "[~]"
		} else if learned {
			mark = "[x]"
		}
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Mastery +%d", s.Value)
		}
		var cost []string
		if s.CostRed > 0 {
			cost = append(cost, fmt.Sprintf("R%d", s.CostRed))
		}
		if s.CostGreen > 0 {
			cost = append(cost, fmt.Sprintf("G%d", s.CostGreen))
		}
		if s.CostBlue > 0 {
			cost = append(cost, fmt.Sprintf("B%d", s.CostBlue))
		}
		label := fmt.Sprintf("%s %s", mark, name)
		if branch == "" {
			label += "   - " + names[s.Branch]
		}
		if len(cost) > 0 {
			label += "   (cost " + strings.Join(cost, " ") + ")"
		}
		ed.shownSkillIDs = append(ed.shownSkillIDs, s.ID)
		labels = append(labels, label)
	}
	ed.skillModel.labels = labels
	ed.skillModel.PublishItemsReset()
	if sel >= len(labels) {
		sel = len(labels) - 1
	}
	ed.skills.SetCurrentIndex(sel)
}

// ---------- actions ----------

func (ed *zombieEditor) equip(slot string, cb *walk.ComboBox, ids []string) {
	i := cb.CurrentIndex()
	if i < 0 || i >= len(ids) {
		return
	}
	if ids[i] == "" {
		ed.do("UNEQUIP " + slot)
		return
	}
	ed.do("EQUIP " + slot + " " + ids[i])
}

func (ed *zombieEditor) removeSelected() {
	if it := ed.selectedBodyItem(); it != nil {
		ed.do("REMOVE " + it.UID)
	}
}

func (ed *zombieEditor) replaceSelected() {
	it := ed.selectedBodyItem()
	i := ed.replaceList.CurrentIndex()
	if it != nil && i >= 0 && i < len(ed.replaceIDs) {
		ed.pendingSelectID = ed.replaceIDs[i]
		ed.do("REPLACE " + it.UID + " " + ed.replaceIDs[i])
	}
}

func (ed *zombieEditor) selectedSkill() string {
	i := ed.skills.CurrentIndex()
	if i < 0 || i >= len(ed.shownSkillIDs) {
		return ""
	}
	return ed.shownSkillIDs[i]
}

func (ed *zombieEditor) skillSelected(learn bool) {
	if id := ed.selectedSkill(); id != "" {
		ed.do(fmt.Sprintf("PERK %s %d", id, boolInt(learn)))
	}
}

func (ed *zombieEditor) skillToggle() {
	if id := ed.selectedSkill(); id != "" {
		learned, _ := ed.info.Learned(id)
		ed.do(fmt.Sprintf("PERK %s %d", id, boolInt(!learned)))
	}
}

func (ed *zombieEditor) skillAll(learn bool) {
	var cmds []string
	for _, id := range ed.shownSkillIDs {
		if learned, _ := ed.info.Learned(id); learned != learn {
			cmds = append(cmds, fmt.Sprintf("PERK %s %d", id, boolInt(learn)))
		}
	}
	if len(cmds) == 0 {
		ed.status.SetText("Nothing to change.")
		return
	}
	ed.doMany(cmds)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// do sends one ZOMBIE sub-command ("" = just reload) and refreshes the window.
func (ed *zombieEditor) do(cmd string) {
	if cmd == "" {
		ed.doMany(nil)
		return
	}
	ed.doMany([]string{cmd})
}

func (ed *zombieEditor) doMany(cmds []string) {
	if !ed.busy.CompareAndSwap(false, true) {
		return
	}
	ed.status.SetText("Working…")
	if it := ed.selectedBodyItem(); it != nil {
		ed.selectUID = it.UID
	}
	ed.selectID, ed.pendingSelectID = ed.pendingSelectID, ""
	go func() {
		last, failed := "", false
		for _, c := range cmds {
			ok, msg := ed.send(c)
			last = msg
			if !ok {
				failed = true
				break
			}
		}
		info, err := ed.get()
		ed.dlg.Synchronize(func() {
			ed.busy.Store(false)
			switch {
			case err != nil:
				ed.status.SetText("✖ " + err.Error())
				return
			case failed:
				ed.status.SetText("✖ " + strings.TrimPrefix(last, "ERR "))
			case len(cmds) > 1:
				ed.status.SetText(fmt.Sprintf("✔ %d changes applied", len(cmds)))
			case len(cmds) == 1:
				ed.status.SetText("✔ " + strings.TrimPrefix(last, "OK "))
			default:
				ed.status.SetText("✔ reloaded")
			}
			if len(cmds) > 0 {
				ed.ui.log("zombie " + info.Name + ": " + ed.status.Text())
			}
			ed.info = info
			ed.fillFromInfo()
		})
	}()
}
