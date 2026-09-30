// Package webviewloader - Keeper Trainer fork.
//
// The upstream package embeds WebView2Loader.dll in the executable and, when the DLL isn't on
// disk, maps it from memory with a custom PE loader (go-winloader). Antivirus heuristics treat
// in-memory DLL loading as a malware technique, so this fork removes it completely: the
// Microsoft-signed WebView2Loader.dll must sit next to the .exe and is loaded with the normal
// Windows LoadLibrary. If it's missing, the trainer falls back to its classic window.
package webviewloader

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	nativeModule                                       = windows.NewLazyDLL("WebView2Loader")
	nativeCreate                                       = nativeModule.NewProc("CreateCoreWebView2EnvironmentWithOptions")
	nativeCompareBrowserVersions                       = nativeModule.NewProc("CompareBrowserVersions")
	nativeGetAvailableCoreWebView2BrowserVersionString = nativeModule.NewProc("GetAvailableCoreWebView2BrowserVersionString")
)

// Available reports whether WebView2Loader.dll can be loaded from disk.
func Available() bool {
	return nativeModule.Load() == nil && nativeCreate.Find() == nil
}

// CompareBrowserVersions will compare the 2 given versions and return:
//
//	-1 = v1 < v2
//	 0 = v1 == v2
//	 1 = v1 > v2
func CompareBrowserVersions(v1 string, v2 string) (int, error) {
	_v1, err := windows.UTF16PtrFromString(v1)
	if err != nil {
		return 0, err
	}
	_v2, err := windows.UTF16PtrFromString(v2)
	if err != nil {
		return 0, err
	}
	if err := nativeCompareBrowserVersions.Find(); err != nil {
		return 0, err
	}
	var result int
	_, _, _ = nativeCompareBrowserVersions.Call(
		uintptr(unsafe.Pointer(_v1)),
		uintptr(unsafe.Pointer(_v2)),
		uintptr(unsafe.Pointer(&result)))
	return result, nil
}

// GetInstalledVersion returns the installed version of the webview2 runtime.
// If there is no version installed, a blank string is returned.
func GetInstalledVersion() (string, error) {
	if err := nativeGetAvailableCoreWebView2BrowserVersionString.Find(); err != nil {
		return "", err
	}
	var result *uint16
	_, _, _ = nativeGetAvailableCoreWebView2BrowserVersionString.Call(
		uintptr(unsafe.Pointer(nil)),
		uintptr(unsafe.Pointer(&result)))
	if result == nil {
		return "", nil
	}
	version := windows.UTF16PtrToString(result)
	windows.CoTaskMemFree(unsafe.Pointer(result))
	return version, nil
}

// CreateCoreWebView2EnvironmentWithOptions tries to load WebviewLoader2 and
// call the CreateCoreWebView2EnvironmentWithOptions routine.
func CreateCoreWebView2EnvironmentWithOptions(browserExecutableFolder, userDataFolder *uint16, environmentOptions uintptr, environmentCompletedHandle uintptr) (uintptr, error) {
	if err := nativeCreate.Find(); err != nil {
		return 0, err
	}
	res, _, _ := nativeCreate.Call(
		uintptr(unsafe.Pointer(browserExecutableFolder)),
		uintptr(unsafe.Pointer(userDataFolder)),
		environmentOptions,
		environmentCompletedHandle,
	)
	return res, nil
}

var _ = syscall.Errno(0)
