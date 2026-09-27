//go:build windows

package main

// Loads the GK2Spawner helper DLL into the running game through Unity's Mono
// embedding API:
//
//	domain = mono_get_root_domain()
//	asm    = mono_domain_assembly_open(domain, "<path>\GK2Spawner.dll")
//	image  = mono_assembly_get_image(asm)
//	class  = mono_class_from_name(image, "GK2Spawner", "Bridge")
//	method = mono_class_get_method_from_name(class, "Start", 0)
//	mono_runtime_invoke(method, NULL, NULL, &exception)
//
// Each call runs in its own remote thread via a small x64 stub that attaches
// the thread to Mono, calls the function, stores RAX and detaches again.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	gameProcessName = "GraveyardKeeper2.exe"
	monoModuleName  = "mono-2.0-bdwgc.dll"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procVirtualAllocEx   = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx    = kernel32.NewProc("VirtualFreeEx")
	procCreateRemoteThrd = kernel32.NewProc("CreateRemoteThread")
	monoExports          = []string{
		"mono_get_root_domain", "mono_thread_attach", "mono_thread_detach",
		"mono_domain_assembly_open", "mono_assembly_get_image", "mono_class_from_name",
		"mono_class_get_method_from_name", "mono_runtime_invoke",
		"mono_object_to_string", "mono_string_to_utf8",
	}
)

type remoteProcess struct {
	handle windows.Handle
	mono   map[string]uintptr
	domain uintptr
	allocs []uintptr
}

// injectHelper finds the game, loads dllPath into its Mono domain and calls Bridge.Start().
func injectHelper(dllPath string) error {
	pid, err := findProcess(gameProcessName)
	if err != nil {
		return err
	}
	const access = windows.PROCESS_CREATE_THREAD | windows.PROCESS_QUERY_INFORMATION |
		windows.PROCESS_VM_OPERATION | windows.PROCESS_VM_WRITE | windows.PROCESS_VM_READ
	h, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return fmt.Errorf("OpenProcess(%d): %w (try running as the same user / as administrator)", pid, err)
	}
	defer windows.CloseHandle(h)

	rp := &remoteProcess{handle: h}
	defer rp.freeAll()
	if err := rp.resolveMono(pid); err != nil {
		return err
	}

	rp.domain, err = rp.call(rp.mono["mono_get_root_domain"], false)
	if err != nil {
		return err
	}
	if rp.domain == 0 {
		return errors.New("Mono is not initialised yet - wait until the game's main menu is shown")
	}

	path, err := rp.cString(dllPath)
	if err != nil {
		return err
	}
	asm, err := rp.call(rp.mono["mono_domain_assembly_open"], true, rp.domain, path)
	if err != nil {
		return err
	}
	if asm == 0 {
		return fmt.Errorf("mono_domain_assembly_open failed for %s", dllPath)
	}
	image, err := rp.call(rp.mono["mono_assembly_get_image"], true, asm)
	if err != nil || image == 0 {
		return fmt.Errorf("mono_assembly_get_image failed: %v", err)
	}
	ns, _ := rp.cString("GK2Spawner")
	cls, _ := rp.cString("Bridge")
	class, err := rp.call(rp.mono["mono_class_from_name"], true, image, ns, cls)
	if err != nil || class == 0 {
		return fmt.Errorf("class GK2Spawner.Bridge not found: %v", err)
	}
	start, _ := rp.cString("Start")
	method, err := rp.call(rp.mono["mono_class_get_method_from_name"], true, class, start, 0)
	if err != nil || method == 0 {
		return fmt.Errorf("method Bridge.Start not found: %v", err)
	}
	excSlot, err := rp.alloc(8, windows.PAGE_READWRITE)
	if err != nil {
		return err
	}
	if _, err := rp.call(rp.mono["mono_runtime_invoke"], true, method, 0, 0, excSlot); err != nil {
		return err
	}
	if exc := rp.readPtr(excSlot); exc != 0 {
		return fmt.Errorf("Bridge.Start threw: %s", rp.exceptionText(exc))
	}
	return nil
}

func findProcess(name string) (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		if strings.EqualFold(windows.UTF16ToString(pe.ExeFile[:]), name) {
			return pe.ProcessID, nil
		}
	}
	return 0, fmt.Errorf("%s is not running - start the game first", name)
}

func findModule(pid uint32, name string) (base uintptr, path string, err error) {
	var snap windows.Handle
	for i := 0; i < 10; i++ { // snapshot can fail with ERROR_BAD_LENGTH while modules load
		snap, err = windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return 0, "", fmt.Errorf("module snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	var me windows.ModuleEntry32
	me.Size = uint32(unsafe.Sizeof(me))
	for err = windows.Module32First(snap, &me); err == nil; err = windows.Module32Next(snap, &me) {
		if strings.EqualFold(windows.UTF16ToString(me.Module[:]), name) {
			return me.ModBaseAddr, windows.UTF16ToString(me.ExePath[:]), nil
		}
	}
	return 0, "", fmt.Errorf("%s not loaded in the game (is this the Mono build of the game?)", name)
}

// resolveMono maps the same mono DLL into our process (without running it) to read
// export offsets, then rebases them onto the game's copy.
func (rp *remoteProcess) resolveMono(pid uint32) error {
	remoteBase, path, err := findModule(pid, monoModuleName)
	if err != nil {
		return err
	}
	local, err := windows.LoadLibraryEx(path, 0, windows.DONT_RESOLVE_DLL_REFERENCES)
	if err != nil {
		return fmt.Errorf("LoadLibraryEx(%s): %w", path, err)
	}
	defer windows.FreeLibrary(local)
	rp.mono = map[string]uintptr{}
	for _, name := range monoExports {
		addr, err := windows.GetProcAddress(local, name)
		if err != nil {
			return fmt.Errorf("export %s not found: %w", name, err)
		}
		rp.mono[name] = remoteBase + (addr - uintptr(local))
	}
	return nil
}

func (rp *remoteProcess) alloc(size int, protect uint32) (uintptr, error) {
	addr, _, err := procVirtualAllocEx.Call(uintptr(rp.handle), 0, uintptr(size),
		windows.MEM_COMMIT|windows.MEM_RESERVE, uintptr(protect))
	if addr == 0 {
		return 0, fmt.Errorf("VirtualAllocEx: %w", err)
	}
	rp.allocs = append(rp.allocs, addr)
	return addr, nil
}

func (rp *remoteProcess) freeAll() {
	for _, a := range rp.allocs {
		procVirtualFreeEx.Call(uintptr(rp.handle), a, 0, windows.MEM_RELEASE)
	}
	rp.allocs = nil
}

func (rp *remoteProcess) write(addr uintptr, data []byte) error {
	var n uintptr
	return windows.WriteProcessMemory(rp.handle, addr, &data[0], uintptr(len(data)), &n)
}

func (rp *remoteProcess) read(addr uintptr, size int) []byte {
	buf := make([]byte, size)
	var n uintptr
	if windows.ReadProcessMemory(rp.handle, addr, &buf[0], uintptr(size), &n) != nil {
		return nil
	}
	return buf[:n]
}

func (rp *remoteProcess) readPtr(addr uintptr) uintptr {
	b := rp.read(addr, 8)
	if len(b) != 8 {
		return 0
	}
	return uintptr(binary.LittleEndian.Uint64(b))
}

func (rp *remoteProcess) cString(s string) (uintptr, error) {
	addr, err := rp.alloc(len(s)+1, windows.PAGE_READWRITE)
	if err != nil {
		return 0, err
	}
	return addr, rp.write(addr, append([]byte(s), 0))
}

// exceptionText turns a MonoException* into its ToString() text.
func (rp *remoteProcess) exceptionText(exc uintptr) string {
	str, err := rp.call(rp.mono["mono_object_to_string"], true, exc, 0)
	if err != nil || str == 0 {
		return "unknown exception"
	}
	utf8, err := rp.call(rp.mono["mono_string_to_utf8"], true, str)
	if err != nil || utf8 == 0 {
		return "unknown exception"
	}
	b := rp.read(utf8, 2048)
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// call runs fn(args...) (up to 4 pointer-sized args) in a new remote thread and returns RAX.
func (rp *remoteProcess) call(fn uintptr, attach bool, args ...uintptr) (uintptr, error) {
	if len(args) > 4 {
		return 0, errors.New("call: at most 4 arguments")
	}
	mem, err := rp.alloc(256, windows.PAGE_EXECUTE_READWRITE)
	if err != nil {
		return 0, err
	}
	result := mem + 248
	code := buildStub(fn, result, attach, rp.domain, rp.mono["mono_thread_attach"], rp.mono["mono_thread_detach"], args)
	if err := rp.write(mem, code); err != nil {
		return 0, err
	}
	th, _, err := procCreateRemoteThrd.Call(uintptr(rp.handle), 0, 0, mem, 0, 0, 0)
	if th == 0 {
		return 0, fmt.Errorf("CreateRemoteThread: %w", err)
	}
	defer windows.CloseHandle(windows.Handle(th))
	if ev, _ := windows.WaitForSingleObject(windows.Handle(th), 20000); ev != windows.WAIT_OBJECT_0 {
		return 0, errors.New("remote call timed out")
	}
	return rp.readPtr(result), nil
}

// buildStub emits Windows x64 code:
//
//	sub rsp,38h
//	[mov rcx,domain; mov rax,mono_thread_attach; call rax; mov [rsp+28h],rax]
//	mov rcx,a0; mov rdx,a1; mov r8,a2; mov r9,a3; mov rax,fn; call rax
//	mov [result],rax
//	[mov rcx,[rsp+28h]; mov rax,mono_thread_detach; call rax]
//	add rsp,38h; xor eax,eax; ret
func buildStub(fn, result uintptr, attach bool, domain, attachFn, detachFn uintptr, args []uintptr) []byte {
	var c []byte
	imm := func(prefix []byte, v uintptr) {
		c = append(c, prefix...)
		c = binary.LittleEndian.AppendUint64(c, uint64(v))
	}
	callRax := []byte{0xFF, 0xD0}
	c = append(c, 0x48, 0x83, 0xEC, 0x38)
	if attach {
		imm([]byte{0x48, 0xB9}, domain)
		imm([]byte{0x48, 0xB8}, attachFn)
		c = append(c, callRax...)
		c = append(c, 0x48, 0x89, 0x44, 0x24, 0x28)
	}
	regs := [][]byte{{0x48, 0xB9}, {0x48, 0xBA}, {0x49, 0xB8}, {0x49, 0xB9}}
	for i, a := range args {
		imm(regs[i], a)
	}
	imm([]byte{0x48, 0xB8}, fn)
	c = append(c, callRax...)
	imm([]byte{0x48, 0xA3}, result) // movabs [result], rax
	if attach {
		c = append(c, 0x48, 0x8B, 0x4C, 0x24, 0x28)
		imm([]byte{0x48, 0xB8}, detachFn)
		c = append(c, callRax...)
	}
	c = append(c, 0x48, 0x83, 0xC4, 0x38, 0x31, 0xC0, 0xC3)
	return c
}

// gameRunning reports whether the game process exists (used by the auto-connect loop).
func gameRunning() bool {
	_, err := findProcess(gameProcessName)
	return err == nil
}
