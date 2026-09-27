//go:build !windows

// Command-line fallback for non-Windows builds (the game itself is Windows-only).
// Useful for testing against a helper: gk2_item_spawner add <id> <count> | status
package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: gk2_item_spawner status | add <itemId> <count>")
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
		msg = map[helperState]string{helperMissing: "not connected", helperOutdated: "old helper loaded", helperReady: "connected (helper v1)"}[probeHelper()]
		ok = true
	case os.Args[1] == "add" && len(os.Args) == 4:
		ok, msg = addItem(os.Args[2], num(3))
	default:
		fmt.Println("usage: gk2_item_spawner status | add <itemId> <count>")
		os.Exit(2)
	}
	fmt.Println(msg)
	if !ok {
		os.Exit(1)
	}
}
