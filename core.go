// Shared logic for GK2 Item Spawner: item list, the in-game helper DLL and the
// line protocol spoken with it (see payload/Bridge.cs).
package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed items.json
var itemsJSON []byte

//go:embed build/GK2Spawner.dll
var helperDLL []byte

const (
	appVersion    = "3.0"
	helperAddr    = "127.0.0.1:27817" // must match Bridge.Port in payload/Bridge.cs
	helperVersion = "VERSION 9"       // must match Bridge.Version
	outdatedMsg   = "The game still has an older helper loaded. Restart the game and load your save - the spawner reconnects by itself."
	maxCount      = 9999
	techCap       = 999
	moneyMax      = 999999999 // copper; game limit of the "money" resource
)

// Item is one entry of items.json (generated from the game's GameBalance + English texts).
type Item struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Detail string   `json:"detail"`
	Stack  int      `json:"stack"`
	Groups []string `json:"groups"`
}

// Label is how an item is shown in the list.
func (it Item) Label() string {
	if it.Name == it.ID {
		return it.ID
	}
	if it.Detail != "" {
		return fmt.Sprintf("%s — %s   (%s)", it.Name, it.Detail, it.ID)
	}
	return fmt.Sprintf("%s   (%s)", it.Name, it.ID)
}

func loadItems() ([]Item, error) {
	var items []Item
	err := json.Unmarshal(itemsJSON, &items)
	return items, err
}

// helperState is the result of asking the game-side helper for its version.
type helperState int

const (
	helperMissing  helperState = iota // nothing listening: not injected yet (or game closed)
	helperOutdated                    // an older helper version is loaded in the game
	helperReady
)

func probeHelper() helperState {
	reply, err := sendCommand("VERSION")
	switch {
	case err != nil:
		return helperMissing
	case reply != helperVersion:
		return helperOutdated
	default:
		return helperReady
	}
}

var bridgeMu sync.Mutex // one request to the game at a time

// sendCommand sends one line to the in-game helper and returns its one-line reply.
func sendCommand(line string) (string, error) {
	bridgeMu.Lock()
	defer bridgeMu.Unlock()
	conn, err := net.DialTimeout("tcp", helperAddr, 500*time.Millisecond)
	if err != nil {
		return "", errors.New("helper not running in the game")
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", line); err != nil {
		return "", err
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply), nil
}

// addItem asks the game to add count × id. ok reports whether the game accepted it.
func addItem(id string, count int) (ok bool, message string) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, " \r\n") {
		return false, "choose an item first"
	}
	if count < 1 || count > maxCount {
		return false, fmt.Sprintf("count must be 1-%d", maxCount)
	}
	return runCommand(fmt.Sprintf("ADD %s %d", id, count))
}

// addTech adds red/green/blue tech points (0,0,0 just reports the current balance).
func addTech(r, g, b int) (ok bool, message string) {
	for _, v := range []int{r, g, b} {
		if v < 0 || v > techCap {
			return false, fmt.Sprintf("tech points must be 0-%d each", techCap)
		}
	}
	return runCommand(fmt.Sprintf("TECH %d %d %d", r, g, b))
}

// changeMoney adds (positive) or removes (negative) copper; 0 just reports the balance.
// 1 silver = 100 copper, 1 gold = 10000 copper. The game keeps money within 0-999999999.
func changeMoney(copper int) (ok bool, message string) {
	if copper < -moneyMax || copper > moneyMax {
		return false, "amount is too large"
	}
	return runCommand(fmt.Sprintf("MONEY %d", copper))
}

// setInstantCraft turns "one hit finishes the player's craft" on or off in the game.
func setInstantCraft(on bool) (ok bool, message string) {
	if on {
		return runCommand("INSTANT 1")
	}
	return runCommand("INSTANT 0")
}

// ---------- zombie editor (the zombie menu currently open in the game) ----------

// ZombieBodyItem is one item inside a zombie's body: an organ, embalming, pocket item or equipment.
type ZombieBodyItem struct {
	UID   string `json:"uid"`
	ID    string `json:"id"`
	Count int    `json:"count"`
	White int    `json:"white"`
	Red   int    `json:"red"`
	Slot  string `json:"slot"`  // collar | hand | armor | "" (not equipment)
	Group string `json:"group"` // organ group such as gr_heart, "" for others
	Perk  string `json:"perk"`
}

// ZombieTalent is one skill-tree branch of a zombie.
type ZombieTalent struct {
	ID      string   `json:"id"`
	Value   int      `json:"value"`
	Studied []string `json:"studied"`
}

// ZombieInfo is what "ZOMBIE GET" returns.
type ZombieInfo struct {
	Name           string           `json:"name"`
	Type           string           `json:"type"`
	State          string           `json:"state"`
	TechRed        int              `json:"techRed"`
	TechGreen      int              `json:"techGreen"`
	TechBlue       int              `json:"techBlue"`
	White          int              `json:"white"`
	Red            int              `json:"red"`
	PerksUsed      int              `json:"perksUsed"`
	PerksDisabled  int              `json:"perksDisabled"`
	CollarRedMax   int              `json:"collarRedMax"`
	InCollarLimits bool             `json:"inCollarLimits"`
	Items          []ZombieBodyItem `json:"items"`
	Talents        []ZombieTalent   `json:"talents"`
	Disabled       []string         `json:"disabled"`
	// dead bodies only ("CORPSE GET")
	BodyID       string `json:"bodyId"`
	GraveQuality *int   `json:"graveQuality"` // set when the body lies in a grave
}

// Equipped returns the item in an equipment slot, or nil.
func (z *ZombieInfo) Equipped(slot string) *ZombieBodyItem {
	for i := range z.Items {
		if z.Items[i].Slot == slot {
			return &z.Items[i]
		}
	}
	return nil
}

// Learned reports whether a skill-tree node is bought, and whether it is inactive (not enough red skulls).
func (z *ZombieInfo) Learned(id string) (learned, inactive bool) {
	for _, t := range z.Talents {
		for _, s := range t.Studied {
			if s == id {
				learned = true
			}
		}
	}
	for _, s := range z.Disabled {
		if s == id {
			inactive = true
		}
	}
	return learned, inactive
}

// ZombieCatalog lists what can be put on / into a zombie ("ZOMBIE CATALOG").
type ZombieCatalog struct {
	Body []struct {
		ID    string `json:"id"`
		White int    `json:"white"`
		Red   int    `json:"red"`
		Group string `json:"group"`
		Perk  string `json:"perk"`
	} `json:"body"`
	Collars      []string `json:"collars"`
	Hands        []string `json:"hands"`
	Armors       []string `json:"armors"`
	CollarLimits []struct {
		ID     string `json:"id"`
		RedMax int    `json:"redMax"`
	} `json:"collarLimits"`
	Skills []struct {
		ID        string   `json:"id"`
		Branch    string   `json:"branch"`
		Name      string   `json:"name"`
		Perk      string   `json:"perk"`
		Value     int      `json:"value"`
		CostRed   int      `json:"costRed"`
		CostGreen int      `json:"costGreen"`
		CostBlue  int      `json:"costBlue"`
		Parents   []string `json:"parents"`
	} `json:"skills"`
	Branches []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"branches"`
}

// zombieCmd sends "ZOMBIE <args>" (one line; newlines are stripped).
func zombieCmd(args string) (bool, string) {
	args = strings.NewReplacer("\r", " ", "\n", " ").Replace(args)
	return runCommand("ZOMBIE " + args)
}

// zombieOpen reports whether a zombie's menu is open in the game, and its name.
func zombieOpen() (bool, string) {
	ok, msg := zombieCmd("STATE")
	if !ok || !strings.HasPrefix(msg, "OK open") {
		return false, ""
	}
	return true, strings.TrimSpace(strings.TrimPrefix(msg, "OK open"))
}

// ---------- common trainer options ----------

// cheatSet turns a switch on or off: god, stamina, energy, insanity, sleep, gather, freeze.
func cheatSet(name string, on bool) (bool, string) {
	v := 0
	if on {
		v = 1
	}
	return runCommand(fmt.Sprintf("CHEAT SET %s %d", name, v))
}

// cheatSpeed sets "move" (1-3) or "game" (0.25-4) speed.
func cheatSpeed(which string, x float64) (bool, string) {
	return runCommand(fmt.Sprintf("CHEAT SPEED %s %.2f", which, x))
}

func cheatRestore() (bool, string) { return runCommand("CHEAT RESTORE") }

// NPCRep is one NPC's friendship value.
type NPCRep struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func npcGet() ([]NPCRep, error) {
	ok, msg := runCommand("NPC GET")
	if !ok {
		return nil, errors.New(strings.TrimPrefix(msg, "ERR "))
	}
	var out []NPCRep
	err := json.Unmarshal([]byte(strings.TrimPrefix(msg, "OK ")), &out)
	return out, err
}

func npcSet(id string, value int) (bool, string) {
	return runCommand(fmt.Sprintf("NPC SET %s %d", id, value))
}
func npcMax() (bool, string) { return runCommand("NPC MAX") }

// ---------- time fast-forward ----------

var weekdayNames = []string{"", "Gluttony", "Sloth", "Lust", "Envy", "Pride", "Wrath"}

// GameTime is what "TIME GET" reports.
type GameTime struct {
	Day, Weekday int
	TimeOfDay    float64 // 0..1, 0 = midnight (new day), 0.25 dawn, 0.75 dusk
	DayMinutes   float64 // real minutes per game day
	Speed        float64 // current world speed multiplier
	FFTarget     string  // "day@tod" while fast-forwarding, "-" otherwise
}

// Clock returns the time of day as HH:MM.
func (t GameTime) Clock() string {
	m := int(t.TimeOfDay*24*60+0.5) % (24 * 60)
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

func timeGet() (*GameTime, error) {
	ok, msg := runCommand("TIME GET")
	if !ok {
		return nil, errors.New(strings.TrimPrefix(msg, "ERR "))
	}
	var t GameTime
	for _, kv := range strings.Fields(strings.TrimPrefix(msg, "OK ")) {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "day":
			fmt.Sscan(v, &t.Day)
		case "weekday":
			fmt.Sscan(v, &t.Weekday)
		case "tod":
			fmt.Sscan(v, &t.TimeOfDay)
		case "len":
			fmt.Sscan(v, &t.DayMinutes)
		case "speed":
			fmt.Sscan(v, &t.Speed)
		case "ff":
			t.FFTarget = v
		}
	}
	return &t, nil
}

// timeFastForward runs the world at `speed` (2-50, the game sleeps at 50) until `target`
// ("+N" days ahead or a weekday "1".."6") at `hour`:00. rested keeps the lack-of-sleep debuff away.
func timeFastForward(target string, hour int, speed int, rested bool) (bool, string) {
	r := 0
	if rested {
		r = 1
	}
	return runCommand(fmt.Sprintf("TIME FF %s %d %d %d", target, hour, speed, r))
}

func timeStop() (bool, string) { return runCommand("TIME STOP") }

// corpseCmd sends "CORPSE <args>": the dead body on an open autopsy / embalming table or grave,
// or the one the player carries.
func corpseCmd(args string) (bool, string) {
	args = strings.NewReplacer("\r", " ", "\n", " ").Replace(args)
	return runCommand("CORPSE " + args)
}

// corpseOpen reports whether a dead body can be edited now, and a short description of it.
func corpseOpen() (bool, string) {
	ok, msg := corpseCmd("STATE")
	if !ok || !strings.HasPrefix(msg, "OK open") {
		return false, ""
	}
	return true, strings.TrimSpace(strings.TrimPrefix(msg, "OK open"))
}

func corpseGet() (*ZombieInfo, error) {
	ok, msg := corpseCmd("GET")
	if !ok {
		return nil, errors.New(strings.TrimPrefix(msg, "ERR "))
	}
	var z ZombieInfo
	if err := json.Unmarshal([]byte(strings.TrimPrefix(msg, "OK ")), &z); err != nil {
		return nil, err
	}
	return &z, nil
}

func zombieJSON(sub string, out any) error {
	ok, msg := zombieCmd(sub)
	if !ok {
		return errors.New(strings.TrimPrefix(msg, "ERR "))
	}
	return json.Unmarshal([]byte(strings.TrimPrefix(msg, "OK ")), out)
}

func zombieGet() (*ZombieInfo, error) {
	var z ZombieInfo
	if err := zombieJSON("GET", &z); err != nil {
		return nil, err
	}
	return &z, nil
}

func zombieCatalog() (*ZombieCatalog, error) {
	var c ZombieCatalog
	if err := zombieJSON("CATALOG", &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func runCommand(cmd string) (bool, string) {
	switch probeHelper() {
	case helperMissing:
		return false, "not connected to the game yet"
	case helperOutdated:
		return false, outdatedMsg
	}
	reply, err := sendCommand(cmd)
	if err != nil {
		return false, "lost connection to the game: " + err.Error()
	}
	return strings.HasPrefix(reply, "OK"), reply
}

// writeHelperDLL puts the embedded helper somewhere the game process can read it.
func writeHelperDLL() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "gk2_item_spawner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "GK2Spawner.dll")
	if err := os.WriteFile(path, helperDLL, 0o644); err != nil {
		// A game session that already loaded it keeps the file locked; reuse it.
		if _, statErr := os.Stat(path); statErr == nil {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

// connectToGame injects the helper (if needed) and waits until it answers.
func connectToGame() error {
	switch probeHelper() {
	case helperReady:
		return nil
	case helperOutdated:
		return errors.New(outdatedMsg)
	}
	path, err := writeHelperDLL()
	if err != nil {
		return err
	}
	if err := injectHelper(path); err != nil {
		return err
	}
	for i := 0; i < 30; i++ { // the helper starts its listener on a background thread
		switch probeHelper() {
		case helperReady:
			return nil
		case helperOutdated:
			return errors.New(outdatedMsg)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("helper was loaded but does not answer - try again in a few seconds")
}
