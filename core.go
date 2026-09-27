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
	appVersion    = "1.1"
	helperAddr    = "127.0.0.1:27817" // must match Bridge.Port in payload/Bridge.cs
	helperVersion = "VERSION 2"       // must match Bridge.Version
	outdatedMsg   = "The game still has an older helper loaded. Restart the game and load your save - the spawner reconnects by itself."
	maxCount      = 9999
	techCap       = 999
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
