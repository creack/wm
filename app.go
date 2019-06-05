// Copyright 2015 The WM Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package wm is a terminal window manager.
//
// Screenshot
//
// An example content of a terminal window (colors cannot be shown here):
//
//  Use mouse to resize the window or scroll the view.
//  Arrow keys change the viewport of the focused window.
//  To focus the desktop, click on it.
//  <Esc> or 'q' to quit.
//
//                      ┌ view_demo.go ─────────────────────────────────────────────────────[X]┐
//                      │                                                                     ▴│
//                      │                                                                     ░│
//   ┌ view.go ─────────│Use mouse to resize the window or scroll the view.                   ░│
//   │        "github.co│e the viewport of the focused window.                                ▒│
//   │        "github.co│ktop, click on it.                                                   ▒│
//   │)                 │quit.`                                                               ░│
//   │                  │                                                                     ░│
//   │// Meter provides │                                                                     ░│────[X]┐
//   │type Meter interfa│'\n'}                                                                ░│      ▴│
//   │        // Metrics│                                                                     ░│      ░│
//   │        // result │arent *wm.Window, x, y int, title string, src []byte) {              ░│      ░│
//   │        // reflect│rent.Size()                                                          ░│      ░│
//   │        // values │ || y < 0 {                                                          ░│, len(░│
//   │        // output │x = rand.Intn(sz.Width - sz.Width/5)                                 ░│      ░│
//   │        Metrics(vi│y = rand.Intn(sz.Height - sz.Height/5)                               ░│)     ░│
//   │}                 │                                                                     ░│s:%d: ░│
//   │                  │ent.NewChild(wm.Rectangle{wm.Position{x, y}, wm.Size{0, 0}})         ░│      ░│
//   │// View displays c│seButton(true)                                                       ░│      ░│
//   │◂▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒│le(title)                                                            ░│      ░│
//   └──────────────────│.HasSuffix(src, nl) {                                                ░│      ▒│
//                      │src = src[:len(src)-1]                                               ░│      ▒│
//                      │                                                                     ▾│OOK   ▒│
//                      │◂░░░░░░░░░░▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒░░░░░░░░░░░░░░▸ │)     ▒│
//                      └──────────────────────────────────────────────────────────────────────┘%d:\n"▒│
//
//
// Changelog
//
//
// 2016-12-16: Initial release of the accompanying toolkit package:
// http://github.com/creack/wm/tree/master/tk
//
// 2016-11-25: Windows now support views (viewports). See Windows.Origin and
// friends.
//
// 2015-12-11: WM now uses no locks and renders 2 to 3 times faster. The price
// is that any methods of Application, Desktop or Window must be called only
// from a function that was enqueued by Application.Post or
// Application.PostWait.
package wm // import "github.com/creack/wm"

import (
	"fmt"
	rdebug "runtime/debug"
	"sync"
	"time"

	"github.com/gdamore/tcell"
	"github.com/gdamore/tcell/encoding"
)

const (
	anyButton = tcell.Button8<<1 - 1
	anyWheel  = tcell.WheelUp | tcell.WheelDown | tcell.WheelLeft | tcell.WheelRight
)

var (
	// App is the instance of Application created by NewApplication.
	App                *Application
	onceNewApplication sync.Once
)

// Application represents an interactive terminal application.
//
// Application methods must be called only directly from an event handler
// goroutine or from a function that was enqueued using Application.Post or
// Application.PostWait.  The only exception is Application.Wait, it can be
// called from any goroutine.
type Application struct {
	click             time.Duration             //
	desktop           *Desktop                  //
	doubleClick       time.Duration             //
	exitError         error                     //
	mouseButtonFSMs   [8]*mouseButtonFSM        //
	mouseButtonsState tcell.ButtonMask          //
	mouseX            int                       //
	mouseY            int                       //
	onKey             *onKeyHandlerList         //
	onSetClick        *onSetDurationHandlerList //
	onSetDesktop      *onSetDesktopHandlerList  //
	onSetDoubleClick  *onSetDurationHandlerList //
	onSetSize         *OnSetSizeHandlerList     //
	onceExit          sync.Once                 //
	onceFinalize      sync.Once                 //
	onceWait          sync.Once                 //
	screen            tcell.Screen              //
	size              Size                      //
	theme             *Theme                    //
	updateLevel       int32                     //
	wait              chan error                //
}

// NewApplication returns a newly created Application or an error, if any.
//
//	// Skeleton example.
//	func main() {
//		app, err := wm.NewApplication(theme)
//		if err != nil {
//			log.Fatalf("error: %v", err)
//		}
//
//		...
//
//		if err = app.Wait(); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// Calling this function more than once will panic.
func NewApplication(theme *Theme) (*Application, error) {
	done := false
	var app *Application
	var err error
	onceNewApplication.Do(func() {
		app, err = newApplication(nil, theme)
		done = true
	})
	if !done {
		panic("NewApplication called more than once")
	}

	return app, err
}

func newApplication(screen tcell.Screen, t *Theme) (*Application, error) {
	encoding.Register()
	var err error
	if screen == nil {
		if screen, err = tcell.NewScreen(); err != nil {
			return nil, err
		}
	}

	if err = screen.Init(); err != nil {
		return nil, err
	}

	var size Size
	size.Width, size.Height = screen.Size()
	theme := *t
	App = &Application{
		click:       150 * time.Millisecond,
		doubleClick: 120 * time.Millisecond,
		screen:      screen,
		size:        size,
		theme:       &theme,
		wait:        make(chan error, 1),
	}

	mask := tcell.Button1
	for i := range App.mouseButtonFSMs {
		App.mouseButtonFSMs[i] = newMouseButtonFSM(mask)
		mask <<= 1
	}
	App.screen.EnableMouse()
	App.OnKey(App.onKeyHandler, nil)
	App.OnSetDesktop(App.onSetDesktopHandler, nil)
	App.OnSetSize(App.onSetSizeHandler, nil)
	go App.handleEvents()
	return App, nil
}

func (a *Application) handleEvents() {
	defer func() {
		if err := recover(); err != nil {
			a.finalize()
			a.Exit(fmt.Errorf("PANIC: %v\n%s", err, rdebug.Stack()))
		}
	}()

	for {
		ev := a.screen.PollEvent()
		if ev == nil {
			return
		}

		d := a.desktop
		var r *Window
		if d != nil {
			r = d.root
		}
		if r != nil {
			r.BeginUpdate()
		}

		switch e := ev.(type) {
		case *tcell.EventResize:
			a.setSize(newSize(e.Size()))
		case *tcell.EventKey:
			a.onKey.handle(nil, e.Key(), e.Modifiers(), e.Rune())
		case *tcell.EventMouse:
			x, y := e.Position()
			btn := e.Buttons()
			if x != a.mouseX || y != a.mouseY || btn&anyWheel != 0 {
				a.mouseX = x
				a.mouseY = y
				a.screen.PostEvent(newEventMouse(mouseMove, btn, e.Modifiers(), Position{x, y}))
			}
			if b := btn & anyButton; b != a.mouseButtonsState {
				diff := b ^ a.mouseButtonsState
				a.mouseButtonsState = b
				x := 0
				for diff != 0 {
					if diff&1 != 0 {
						a.mouseButtonFSMs[x].post(e)
					}
					diff >>= 1
					x++
				}
			}
		case *eventMouse:
			w := a.Desktop().Root()
			switch e.kind {
			case mouseDrag:
				w.drag(e.button, e.Position, e.mods)
			case mouseDrop:
				w.drop(e.button, e.Position, e.mods)
			case mouseClick:
				w.click(e.button, e.Position, e.mods)
			case mouseDoubleClick:
				w.doubleClick(e.button, e.Position, e.mods)
			case mouseMove:
				w.mouseMove(e.button, e.Position, e.mods)
			default:
				panic(fmt.Errorf("%v", e.kind))
			}
			e.dispose()
		case *eventFunc:
			e.f()
			e.dispose()
		default:
			panic(fmt.Errorf("%T", e))
		}

		if r != nil {
			r.EndUpdate()
		}
	}
}

func (a *Application) onSetDesktopHandler(_ *Window, prev OnSetDesktopHandler, dst **Desktop, src *Desktop) {
	if prev != nil {
		prev(nil, nil, dst, src)
	} else {
		*dst = src
	}

	w := a.Desktop().Root()
	w.setSize(a.Size())
	w.Invalidate(w.Area())
}

func (a *Application) onKeyHandler(w *Window, prev OnKeyHandler, key tcell.Key, mod tcell.ModMask, r rune) bool {
	if prev != nil {
		panic("internal error")
	}

	d := a.Desktop()
	if d == nil {
		return true
	}

	fw := d.FocusedWindow()
	if fw == nil {
		return false
	}

	return fw.onKey.handle(fw, key, mod, r)
}

func (a *Application) onSetSizeHandler(_ *Window, prev OnSetSizeHandler, dst *Size, src Size) {
	if prev != nil {
		prev(nil, nil, dst, src)
	} else {
		*dst = src
	}

	d := a.Desktop()
	if d == nil {
		return
	}

	sz := a.Size()
	w := d.Root()
	w.setSize(sz)
}

func (a *Application) paintSelection() {
	d := a.Desktop()
	if d == nil {
		return
	}

	area := d.Selection()
	if area.IsZero() {
		return
	}

	o := area.Position
	for y := 0; y < area.Height; y++ {
		sy := o.Y + y
		fx := true
		for x := 0; x < area.Width; x++ {
			sx := o.X + x
			if fx {
				_, _, _, width := a.screen.GetContent(sx-1, sy)
				if width == 2 {
					sx--
					x--
				}
			}
			fx = false
			mainc, combc, style, width := a.screen.GetContent(sx, sy)
			style ^= tcell.Style(tcell.AttrReverse)
			a.screen.SetContent(sx, sy, mainc, combc, style)
			if width == 2 {
				x++
			}
		}
	}
}

var marker = Style{Background: tcell.ColorRed, Foreground: tcell.ColorBlack}

func (a *Application) setCell(p Position, mainc rune, combc []rune, style tcell.Style) {
	switch {
	case debug:
		// Make screen updates slow enough for human observation.
		a.screen.SetContent(p.X, p.Y, tcell.RuneDiamond, nil, marker.TCellStyle())
		a.screen.Show()
		a.screen.SetContent(p.X, p.Y, mainc, combc, style)
		a.screen.Show()
		a.screen.SetContent(p.X, p.Y, tcell.RuneDiamond, nil, marker.TCellStyle())
		a.screen.Show()
		a.screen.SetContent(p.X, p.Y, mainc, combc, style)
		a.screen.Show()
	default:
		a.screen.SetContent(p.X, p.Y, mainc, combc, style)
	}
}

func (a *Application) finalize() { a.onceFinalize.Do(func() { a.screen.Fini() }) }

// ----------------------------------------------------------------------------

// BeginUpdate marks the start of one or more updates to the application
// screen.
//
// Failing to properly pair BeginUpdate with a corresponding EndUpdate will
// cause application screen corruption and/or freeze.
func (a *Application) BeginUpdate() {
	a.updateLevel++
	if a.updateLevel == 1 {
		a.paintSelection() // Remove selection.
	}
}

// ChildWindowStyle returns the style assigned to new child windows.
func (a *Application) ChildWindowStyle() WindowStyle { return a.theme.ChildWindow }

// ClickDuration returns the maximum duration of a single click. Holding a
// mouse button for any longer duration generates a drag event instead.
func (a *Application) ClickDuration() time.Duration { return a.click }

// Colors returns the number of colors the host terminal supports.  All colors
// are assumed to use the ANSI color map.  If a terminal is monochrome, it will
// return 0.
func (a *Application) Colors() int { return a.screen.Colors() }

// Desktop returns the currently active desktop.
func (a *Application) Desktop() (d *Desktop) { return a.desktop }

// DesktopStyle returns the style assigned to new desktops.
func (a *Application) DesktopStyle() WindowStyle { return a.theme.Desktop }

// DoubleClickDuration returns the maximum duration of a double click. Mouse
// click not followed by another one within the DoubleClickDuration is a single
// click.
func (a *Application) DoubleClickDuration() time.Duration { return a.doubleClick }

// EndUpdate marks the end of one or more updates to the application screen.
//
// Failing to properly pair BeginUpdate with a corresponding EndUpdate will
// cause application screen corruption and/or freeze.
func (a *Application) EndUpdate() {
	a.updateLevel--
	if a.updateLevel == 0 {
		a.paintSelection() // Show selection.
		a.screen.Show()
	}
}

// Exit terminates the interactive terminal application and returns err from
// Wait(). Only the first call of this method is considered.
func (a *Application) Exit(err error) {
	a.finalize()
	a.onceExit.Do(func() { a.wait <- err })
}

// NewDesktop returns a newly created desktop.
func (a *Application) NewDesktop() *Desktop { return newDesktop() }

// OnKey sets a key event handler. When the event handler is removed, finalize
// is called, if not nil.
func (a *Application) OnKey(h OnKeyHandler, finalize func()) {
	addOnKeyHandler(&a.onKey, h, finalize)
}

// OnSetClickDuration sets a handler invoked on SetClickDuration. When the
// event handler is removed, finalize is called, if not nil.
func (a *Application) OnSetClickDuration(h OnSetDurationHandler, finalize func()) {
	addOnSetDurationHandler(&a.onSetClick, h, finalize)
}

// OnSetDesktop sets a handler invoked on SetDesktop. When the event handler is
// removed, finalize is called, if not nil.
func (a *Application) OnSetDesktop(h OnSetDesktopHandler, finalize func()) {
	addOnSetDesktopHandler(&a.onSetDesktop, h, finalize)
}

// OnSetDoubleClickDuration sets a handler invoked on SetDoubleClickDuration.
// When the event handler is removed, finalize is called, if not nil.
func (a *Application) OnSetDoubleClickDuration(h OnSetDurationHandler, finalize func()) {
	addOnSetDurationHandler(&a.onSetDoubleClick, h, finalize)
}

// OnSetSize sets a handler invoked on resizing the application screen. When
// the event handler is removed, finalize is called, if not nil.
func (a *Application) OnSetSize(h OnSetSizeHandler, finalize func()) {
	AddOnSetSizeHandler(&a.onSetSize, h, finalize)
}

// Post puts f in the event queue, if the queue is not full, and executes it on
// dequeuing the event.
func (a *Application) Post(f func()) { a.screen.PostEvent(newEventFunc(f)) }

// PostWait puts f in the event queue and executes it on dequeuing the event.
func (a *Application) PostWait(f func()) { a.screen.PostEventWait(newEventFunc(f)) }

// RemoveOnKey undoes the most recent OnKey call. The function will panic if
// there is no handler set.
func (a *Application) RemoveOnKey() { removeOnKeyHandler(&a.onKey) }

// RemoveOnSetClickDuration undoes the most recent OnSetClickDuration call. The
// function will panic if there is no handler set.
func (a *Application) RemoveOnSetClickDuration() { removeOnSetDurationHandler(&a.onSetClick) }

// RemoveOnSetDesktop undoes the most recent OnSetDesktop call. The function
// will panic if there is no handler set.
func (a *Application) RemoveOnSetDesktop() { removeOnSetDesktopHandler(&a.onSetDesktop) }

// RemoveOnSetDoubleClickDuration undoes the most recent
// OnSetDoubleClickDuration call. The function will panic if there is no
// handler set.
func (a *Application) RemoveOnSetDoubleClickDuration() {
	removeOnSetDurationHandler(&a.onSetDoubleClick)
}

// RemoveOnSetSize undoes the most recent OnSetSize call. The function
// will panic if there is no handler set.
func (a *Application) RemoveOnSetSize() { RemoveOnSetSizeHandler(&a.onSetSize) }

// Run is a shorthand for
//
//	app.PostWait(setup)
//	return app.Wait()
func (a *Application) Run(setup func()) error {
	a.PostWait(setup)
	return a.Wait()
}

// SetClickDuration sets the maximum duration of a single click. Holding a
// mouse button for any longer duration generates a drag event instead.
func (a *Application) SetClickDuration(d time.Duration) { a.onSetClick.handle(nil, &a.click, d) }

// SetDesktop sets the currently active desktop. Passing nil d will panic.
func (a *Application) SetDesktop(d *Desktop) {
	if d == nil {
		panic("cannot set nil desktop")
	}

	a.onSetDesktop.handle(nil, &a.desktop, d)
}

// SetDoubleClickDuration sets the maximum duration of a single click. Mouse
// click not followed by another one within the DoubleClickDuration is a single
// click.
//
// Note: Setting DoubleClickDuration to zero disables double click support.
func (a *Application) SetDoubleClickDuration(d time.Duration) {
	a.onSetClick.handle(nil, &a.doubleClick, d)
}

func (a *Application) setSize(s Size) { a.onSetSize.Handle(nil, &a.size, s) }

// Size returns the size of the terminal the application runs in.
func (a *Application) Size() (s Size) { return a.size }

// Sync updates every character cell of the application screen.
func (a *Application) Sync() { a.screen.Sync() }

// Wait blocks until the interactive terminal application terminates.
//
// Calling this method more than once will panic.
func (a *Application) Wait() error {
	err := a.exitError
	a.onceWait.Do(func() { err = <-a.wait })
	return err
}
