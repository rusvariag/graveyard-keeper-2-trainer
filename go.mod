module gk2spawner

go 1.26.1

require (
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/lxn/walk v0.0.0-20210112085537-c389da54e794
	golang.org/x/sys v0.48.0
)

require (
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
)

// Fork without in-memory DLL loading (see third_party/go-webview2/webviewloader/module.go)
replace github.com/jchv/go-webview2 => ./third_party/go-webview2
