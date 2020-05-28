/*
Package systray is a cross-platform Go library to place an icon and menu in the notification area.
*/
package systray

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/getlantern/golog"
)

type MenuItems map[int32]*MenuItem

var (
	log = golog.LoggerFor("systray")

	systrayReady  func()
	systrayExit   func()
	menuItems     = make(MenuItems)
	menuItemsLock sync.RWMutex

	currentID = int32(-1)
	quitOnce  sync.Once
)

func init() {
	runtime.LockOSThread()
}

// MenuItem is used to keep track each menu item of systray.
// Don't create it directly, use the one systray.AddMenuItem() returned
type MenuItem struct {
	// ClickedCh is the channel which will be notified when the menu item is clicked
	ClickedCh chan struct{}

	// id uniquely identify a menu item, not supposed to be modified
	id int32
	// title is the text shown on menu item
	title string
	// tooltip is the text shown when pointing to menu item
	tooltip string
	// disabled menu item is grayed out and has no effect when clicked
	disabled bool
	// checked menu item has a tick before the title
	checked bool
	// parent item, for sub menus
	parent *MenuItem
}

func (item *MenuItem) String() string {
	if item.parent == nil {
		return fmt.Sprintf("MenuItem[%d, %q]", item.id, item.title)
	}
	return fmt.Sprintf("MenuItem[%d, parent %d, %q]", item.id, item.parent.id, item.title)
}

// newMenuItem returns a populated MenuItem object
func newMenuItem(title string, tooltip string, parent *MenuItem) *MenuItem {
	return &MenuItem{
		ClickedCh: make(chan struct{}),
		id:        atomic.AddInt32(&currentID, 1),
		title:     title,
		tooltip:   tooltip,
		disabled:  false,
		checked:   false,
		parent:    parent,
	}
}

func GetMenuItems() MenuItems {
	return menuItems
}

func (t *MenuItems) Reset() {
	menuItemsLock.Lock()

	for key, item := range *t {
		hideMenuItem(item)
		delete(*t, key)
	}

	menuItemsLock.Unlock()
}

func (t *MenuItems) Remove(item *MenuItem) {
	menuItemsLock.Lock()

	hideMenuItem(item)
	delete(menuItems, item.id)

	menuItemsLock.Unlock()
}

// Run initializes GUI and starts the event loop, then invokes the onReady
// callback. It blocks until systray.Quit() is called.
func Run(onReady func(), onExit func()) {
	Register(onReady, onExit)
	nativeLoop()
}

// Register initializes GUI and registers the callbacks but relies on the
// caller to run the event loop somewhere else. It's useful if the program
// needs to show other UI elements, for example, webview.
func Register(onReady func(), onExit func()) {
	if onReady == nil {
		systrayReady = func() {}
	} else {
		// Run onReady on separate goroutine to avoid blocking event loop
		readyCh := make(chan interface{})
		go func() {
			<-readyCh
			onReady()
		}()
		systrayReady = func() {
			close(readyCh)
		}
	}
	// unlike onReady, onExit runs in the event loop to make sure it has time to
	// finish before the process terminates
	if onExit == nil {
		onExit = func() {}
	}
	systrayExit = onExit
	registerSystray()
}

// Quit the systray
func Quit() {
	quitOnce.Do(quit)
}

// AddMenuItem adds a menu item with the designated title and tooltip.
//
// It can be safely invoked from different goroutines.
func AddMenuItem(title string, tooltip string) *MenuItem {
	item := newMenuItem(title, tooltip, nil)
	item.update()
	return item
}

// AddSeparator adds a separator bar to the menu
func AddSeparator() {
	addSeparator(atomic.AddInt32(&currentID, 1))
}

// AddSubMenuItem adds a nested sub-menu item with the designated title and tooltip.
// It can be safely invoked from different goroutines.
func (item *MenuItem) AddSubMenuItem(title string, tooltip string) *MenuItem {
	child := newMenuItem(title, tooltip, item)
	child.update()
	return child
}

// SetTitle set the text to display on a menu item
func (item *MenuItem) SetTitle(title string) {
	item.title = title
	item.update()
}

// SetTooltip set the tooltip to show when mouse hover
func (item *MenuItem) SetTooltip(tooltip string) {
	item.tooltip = tooltip
	item.update()
}

// Disabled checks if the menu item is disabled
func (item *MenuItem) Disabled() bool {
	return item.disabled
}

// Enable a menu item regardless if it's previously enabled or not
func (item *MenuItem) Enable() {
	item.disabled = false
	item.update()
}

// Disable a menu item regardless if it's previously disabled or not
func (item *MenuItem) Disable() {
	item.disabled = true
	item.update()
}

// Hide hides a menu item
func (item *MenuItem) Hide() {
	hideMenuItem(item)
}

// Show shows a previously hidden menu item
func (item *MenuItem) Show() {
	showMenuItem(item)
}

// Checked returns if the menu item has a check mark
func (item *MenuItem) Checked() bool {
	return item.checked
}

// Check a menu item regardless if it's previously checked or not
func (item *MenuItem) Check() {
	item.checked = true
	item.update()
}

// Uncheck a menu item regardless if it's previously unchecked or not
func (item *MenuItem) Uncheck() {
	item.checked = false
	item.update()
}

// update propagates changes on a menu item to systray
func (item *MenuItem) update() {
	menuItemsLock.Lock()
	menuItems[item.id] = item
	menuItemsLock.Unlock()
	addOrUpdateMenuItem(item)
}

func systrayMenuItemSelected(id int32) {
	menuItemsLock.RLock()
	item, ok := menuItems[id]
	menuItemsLock.RUnlock()
	if !ok {
		log.Errorf("No menu item with ID %v", id)
		return
	}
	select {
	case item.ClickedCh <- struct{}{}:
	// in case no one waiting for the channel
	default:
	}
}
