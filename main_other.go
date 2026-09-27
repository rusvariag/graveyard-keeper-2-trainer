//go:build !windows

// Command-line fallback for non-Windows builds (the game itself is Windows-only).
// Useful for testing against a helper: gk2_item_spawner add <id> <count> | tech <r> <g> <b> | status
package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: gk2_item_spawner status | add <itemId> <count> | tech <red> <green> <blue>")
		os.Exit(2)
	}
	num := func(i int) int {
		n, err := strconv.Atoi(os.Args[i])
		if err != nil {
			fmt.Println("not a number:", os.Args[i])
			os.Exit(2)
		}
		return n
	}
	var ok bool
	var msg string
	switch {
	case os.Args[1] == "status":
		msg = map[helperState]string{helperMissing: "not connected", helperOutdated: "old helper loaded", helperReady: "connected (helper v2)"}[probeHelper()]
		ok = true
	case os.Args[1] == "add" && len(os.Args) == 4:
		ok, msg = addItem(os.Args[2], num(3))
	case os.Args[1] == "tech" && len(os.Args) == 5:
		ok, msg = addTech(num(2), num(3), num(4))
	default:
		fmt.Println("usage: gk2_item_spawner status | add <itemId> <count> | tech <red> <green> <blue>")
		os.Exit(2)
	}
	fmt.Println(msg)
	if !ok {
		os.Exit(1)
	}
}
