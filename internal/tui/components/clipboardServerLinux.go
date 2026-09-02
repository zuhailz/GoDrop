//go:build linux

package components

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// copyViaX11Server hands the text to a detached copy of this binary that
// takes ownership of the X11 CLIPBOARD selection and serves paste requests
// for as long as it stays the owner -- the xclip architecture, in-process,
// with no external tools required. It covers plain X11 sessions, and under
// XWayland the clipboard is bridged into the Wayland session. Ownership is
// confirmed through a handshake before success is reported.
func copyViaX11Server(text string) bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(),
		clipboardServerEnv+"=1",
		clipboardContentEnv+"="+text,
	)
	reader, writer, err := os.Pipe()
	if err != nil {
		return false
	}
	cmd.Stdout = writer
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlive the session

	if err := cmd.Start(); err != nil {
		reader.Close()
		writer.Close()
		return false
	}
	writer.Close() // the child now holds the only writer end

	okCh := make(chan bool, 1)
	go func() {
		buf := make([]byte, 2)
		n, readErr := reader.Read(buf)
		okCh <- readErr == nil && n == 2 && string(buf[:n]) == "OK"
	}()

	var ok bool
	select {
	case ok = <-okCh:
		reader.Close()
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		reader.Close()
		return false
	}
	if ok {
		_ = cmd.Process.Release() // keep serving after we exit
	}
	return ok
}

// runClipboardServer is the detached helper's entry point: it takes ownership
// of the X11 CLIPBOARD selection, answers paste requests until some other
// application takes the selection, and then exits.
func runClipboardServer(content string) int {
	X, err := xgb.NewConn()
	if err != nil {
		return 1
	}
	defer X.Close()

	screen := xproto.Setup(X).DefaultScreen(X)

	internAtom := func(name string) xproto.Atom {
		reply, err := xproto.InternAtom(X, false, uint16(len(name)), name).Reply()
		if err != nil || reply == nil {
			return 0
		}
		return reply.Atom
	}
	clipboard := internAtom("CLIPBOARD")
	utf8String := internAtom("UTF8_STRING")
	stringAtom := internAtom("STRING")
	textAtom := internAtom("TEXT")
	targets := internAtom("TARGETS")
	timestampProbe := internAtom("GODROP_CLIPBOARD_TIMESTAMP")
	if clipboard == 0 || utf8String == 0 || stringAtom == 0 || textAtom == 0 || targets == 0 || timestampProbe == 0 {
		return 1
	}

	win, err := xproto.NewWindowId(X)
	if err != nil {
		return 1
	}
	if err := xproto.CreateWindowChecked(X, screen.RootDepth, win, screen.Root,
		-1, -1, 1, 1, 0, xproto.WindowClassInputOutput, 0, 0, nil).Check(); err != nil {
		return 1
	}
	if err := xproto.ChangeWindowAttributesChecked(X, win, xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		return 1
	}

	// ICCCM asks for a real timestamp, not CurrentTime: touching a property
	// on our own window hands us one via PropertyNotify.
	if err := xproto.ChangePropertyChecked(X, xproto.PropModeReplace, win, timestampProbe,
		xproto.AtomString, 8, 2, []byte("ok")).Check(); err != nil {
		return 1
	}
	var ts xproto.Timestamp
	for i := 0; ts == 0 && i < 128; i++ {
		ev, xerr := X.WaitForEvent()
		if ev == nil || xerr != nil {
			return 1
		}
		if p, ok := ev.(xproto.PropertyNotifyEvent); ok && p.Atom == timestampProbe {
			ts = p.Time
		}
	}
	if ts == 0 {
		return 1
	}
	xproto.DeleteProperty(X, win, timestampProbe)

	if err := xproto.SetSelectionOwnerChecked(X, win, clipboard, ts).Check(); err != nil {
		return 1
	}
	if owner, err := xproto.GetSelectionOwner(X, clipboard).Reply(); err != nil || owner.Owner != win {
		return 1
	}

	fmt.Println("OK") // handshake: ownership confirmed

	for {
		ev, xerr := X.WaitForEvent()
		if ev == nil || xerr != nil {
			return 0 // the X connection is gone; there is nothing to serve
		}
		switch request := ev.(type) {
		case xproto.SelectionRequestEvent:
			property := request.Property
			if property == 0 {
				property = request.Target
			}
			switch request.Target {
			case targets:
				atomData := []xproto.Atom{utf8String, stringAtom, textAtom, targets}
				buf := make([]byte, 0, len(atomData)*4)
				for _, a := range atomData {
					buf = append(buf, byte(a), byte(a>>8), byte(a>>16), byte(a>>24))
				}
				xproto.ChangeProperty(X, xproto.PropModeReplace, request.Requestor,
					property, targets, 32, uint32(len(atomData)), buf)
			case utf8String, stringAtom, textAtom:
				xproto.ChangeProperty(X, xproto.PropModeReplace, request.Requestor,
					property, request.Target, 8, uint32(len(content)), []byte(content))
			default:
				property = 0 // unsupported target: refuse
			}
			notify := xproto.SelectionNotifyEvent{
				Requestor: request.Requestor,
				Selection: request.Selection,
				Target:    request.Target,
				Property:  property,
				Time:      request.Time,
			}
			xproto.SendEvent(X, false, request.Requestor, 0, string(notify.Bytes()))
			X.Sync()
		case xproto.SelectionClearEvent:
			if request.Selection == clipboard {
				return 0 // another application copied; it owns the selection now
			}
		}
	}
}
