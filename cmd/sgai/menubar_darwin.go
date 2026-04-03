//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include <stdint.h>

static void MenuBarFreeString(char *s) {
    free(s);
}

extern void MenuBarInit(void);
extern void MenuBarSetGoHandle(uintptr_t handle);
extern void MenuBarSetTitle(const char *title);
extern void MenuBarClear(void);
extern void MenuBarAddItem(const char *title, int tag, int enabled);
extern void MenuBarAddSeparator(void);
extern void MenuBarOpenURL(const char *urlStr);
extern void MenuBarRunLoop(void);
extern void MenuBarStop(void);
*/
import "C"

import (
	"context"
	"fmt"
	"runtime/cgo"
)

type darwinMenuBarState struct {
	menuBarState

	baseURL    string
	cancelFunc context.CancelFunc
}

//export goMenuItemClicked
func goMenuItemClicked(handle C.uintptr_t, tag C.int) {
	if handle == 0 {
		return
	}
	clickCh, ok := cgo.Handle(handle).Value().(chan int)
	if !ok {
		return
	}
	select {
	case clickCh <- int(tag):
	default:
	}
}

func startMenuBar(ctx context.Context, baseURL string, srv *Server, cancel context.CancelFunc) {
	clickCh := make(chan int, 1)
	clickHandle := cgo.NewHandle(clickCh)
	defer func() {
		C.MenuBarSetGoHandle(C.uintptr_t(0))
		clickHandle.Delete()
	}()
	C.MenuBarSetGoHandle(C.uintptr_t(clickHandle))

	state := &darwinMenuBarState{
		menuBarState: newMenuBarState(),
		baseURL:      baseURL,
		cancelFunc:   cancel,
	}

	C.MenuBarInit()

	go menuBarClickHandler(ctx, state, clickCh)
	go menuBarUpdateLoop(ctx, srv, state)
	go func() {
		<-ctx.Done()
		C.MenuBarStop()
	}()

	C.MenuBarRunLoop()
}

func menuBarClickHandler(ctx context.Context, state *darwinMenuBarState, clickCh <-chan int) {
	for {
		select {
		case <-ctx.Done():
			return
		case tag := <-clickCh:
			state.mu.Lock()
			action, ok := state.tags[tag]
			cancel := state.cancelFunc
			state.mu.Unlock()
			if !ok {
				continue
			}
			if action.actionURL == "" {
				if cancel != nil {
					cancel()
				}
				continue
			}
			cURL := C.CString(action.actionURL)
			C.MenuBarOpenURL(cURL)
			C.MenuBarFreeString(cURL)
		}
	}
}

func menuBarUpdateLoop(ctx context.Context, srv *Server, state *darwinMenuBarState) {
	sub := srv.signals.subscribe()
	defer srv.signals.unsubscribe(sub)

	rebuildMenuFromServer(srv, state)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.done:
			return
		case <-sub.ch:
			rebuildMenuFromServer(srv, state)
		}
	}
}

func rebuildMenuFromServer(srv *Server, state *darwinMenuBarState) {
	items := menuBarItemsFromServer(srv)

	state.mu.Lock()
	state.nextTag = 0
	state.tags = make(map[int]menuBarAction)
	baseURL := state.baseURL
	state.mu.Unlock()

	runningCount := countRunning(items)
	attentionCount := countAttention(items)
	totalActive := countActive(items)
	setMenuTitle(runningCount, totalActive, attentionCount)

	C.MenuBarClear()

	dashTag := allocTag(&state.menuBarState, menuBarAction{actionURL: baseURL})
	addMenuEntry("Open Dashboard", dashTag, true)

	C.MenuBarAddSeparator()

	for _, item := range filterVisibleItems(items) {
		label := formatMenuItemLabel(item)
		itemURL := workspaceURL(baseURL, item.name, workspaceItemSubpath(item))
		tag := allocTag(&state.menuBarState, menuBarAction{actionURL: itemURL})
		addMenuEntry(label, tag, true)
	}

	C.MenuBarAddSeparator()
	quitTag := allocTag(&state.menuBarState, menuBarAction{actionURL: ""})
	addMenuEntry("Quit", quitTag, true)
}

func setMenuTitle(runningCount, totalActive, attentionCount int) {
	var title string
	switch {
	case totalActive == 0:
		title = "\u25CF sgai"
	case attentionCount > 0:
		title = fmt.Sprintf("\u26A0 %d/%d", runningCount, totalActive)
	default:
		title = fmt.Sprintf("\u25CF %d/%d", runningCount, totalActive)
	}
	cTitle := C.CString(title)
	defer C.MenuBarFreeString(cTitle)
	C.MenuBarSetTitle(cTitle)
}

func addMenuEntry(label string, tag int, enabled bool) {
	cLabel := C.CString(label)
	defer C.MenuBarFreeString(cLabel)
	e := 0
	if enabled {
		e = 1
	}
	C.MenuBarAddItem(cLabel, C.int(tag), C.int(e))
}
