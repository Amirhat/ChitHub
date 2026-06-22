//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

// Quit the whole process when the (only) window is closed.
@interface ChitDelegate : NSObject <NSApplicationDelegate>
@end
@implementation ChitDelegate
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)s { return YES; }
@end

// NSApplication.delegate is a WEAK reference, so a strong one must outlive the
// assignment or ARC deallocates the delegate immediately and the terminate hook
// never fires. A file-scope static is __strong by default — keep it here.
static ChitDelegate *gChitDelegate = nil;

// Menu bridge: menu items carry a JS snippet in representedObject and, when
// chosen, run it in the web view — reusing the UI's own actions. gWebView is
// set in chitRun; both statics are __strong so they outlive menu building.
static WKWebView *gWebView = nil;

@interface ChitActions : NSObject
- (void)runJS:(NSMenuItem *)item;
@end
@implementation ChitActions
- (void)runJS:(NSMenuItem *)item {
  NSString *js = (NSString *)[item representedObject];
  if (js != nil && gWebView != nil)
    [gWebView evaluateJavaScript:js completionHandler:nil];
}
@end
static ChitActions *gActions = nil;

// addJS appends a menu item that runs `js` in the web view when chosen.
static void addJS(NSMenu *m, NSString *title, NSString *key,
                  NSEventModifierFlags mods, NSString *js) {
  NSMenuItem *it = [m addItemWithTitle:title action:@selector(runJS:) keyEquivalent:key];
  [it setTarget:gActions];
  if ([key length] > 0) [it setKeyEquivalentModifierMask:mods];
  [it setRepresentedObject:js];
}

#define CMD NSEventModifierFlagCommand
#define CMDSHIFT (NSEventModifierFlagCommand|NSEventModifierFlagShift)

// The main menu. Besides the standard App/Edit/Window menus (Edit is what makes
// Cmd+C/V/X/A work in the commit box), File/View/Repository expose ChitHub's
// own actions, wired to the web UI via the JS bridge.
static void chitMenu(NSString *name) {
  if (gActions == nil) gActions = [[ChitActions alloc] init];
  NSMenu *bar = [[NSMenu alloc] init];

  // App menu
  NSMenuItem *appI = [[NSMenuItem alloc] init];
  [bar addItem:appI];
  NSMenu *appM = [[NSMenu alloc] init];
  [appM addItemWithTitle:[@"About " stringByAppendingString:name]
                  action:@selector(orderFrontStandardAboutPanel:) keyEquivalent:@""];
  [appM addItem:[NSMenuItem separatorItem]];
  addJS(appM, @"Settings…", @",", CMD, @"chithubMenu.settings()");
  [appM addItem:[NSMenuItem separatorItem]];
  [appM addItemWithTitle:[@"Hide " stringByAppendingString:name] action:@selector(hide:) keyEquivalent:@"h"];
  NSMenuItem *others = [appM addItemWithTitle:@"Hide Others" action:@selector(hideOtherApplications:) keyEquivalent:@"h"];
  [others setKeyEquivalentModifierMask:(NSEventModifierFlagOption|NSEventModifierFlagCommand)];
  [appM addItem:[NSMenuItem separatorItem]];
  [appM addItemWithTitle:[@"Quit " stringByAppendingString:name] action:@selector(terminate:) keyEquivalent:@"q"];
  [appI setSubmenu:appM];

  // File menu
  NSMenuItem *fileI = [[NSMenuItem alloc] init];
  [bar addItem:fileI];
  NSMenu *fileM = [[NSMenu alloc] initWithTitle:@"File"];
  addJS(fileM, @"New Collection…", @"n", CMD, @"chithubMenu.addCollection()");
  addJS(fileM, @"Clone Repository…", @"o", CMDSHIFT, @"chithubMenu.clone()");
  [fileM addItem:[NSMenuItem separatorItem]];
  [fileM addItemWithTitle:@"Close Window" action:@selector(performClose:) keyEquivalent:@"w"];
  [fileI setSubmenu:fileM];

  // Edit menu
  NSMenuItem *editI = [[NSMenuItem alloc] init];
  [bar addItem:editI];
  NSMenu *editM = [[NSMenu alloc] initWithTitle:@"Edit"];
  [editM addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
  NSMenuItem *redo = [editM addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"z"];
  [redo setKeyEquivalentModifierMask:CMDSHIFT];
  [editM addItem:[NSMenuItem separatorItem]];
  [editM addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
  [editM addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
  [editM addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
  [editM addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
  [editI setSubmenu:editM];

  // View menu
  NSMenuItem *viewI = [[NSMenuItem alloc] init];
  [bar addItem:viewI];
  NSMenu *viewM = [[NSMenu alloc] initWithTitle:@"View"];
  addJS(viewM, @"Refresh", @"r", CMD, @"chithubMenu.refresh()");
  addJS(viewM, @"Command Palette…", @"k", CMD, @"chithubMenu.palette()");
  [viewM addItem:[NSMenuItem separatorItem]];
  addJS(viewM, @"Toggle Light / Dark Theme", @"l", CMDSHIFT, @"chithubMenu.toggleTheme()");
  [viewI setSubmenu:viewM];

  // Repository menu
  NSMenuItem *repoI = [[NSMenuItem alloc] init];
  [bar addItem:repoI];
  NSMenu *repoM = [[NSMenu alloc] initWithTitle:@"Repository"];
  addJS(repoM, @"Review Changes…", @"", 0, @"chithubMenu.review()");
  [repoM addItem:[NSMenuItem separatorItem]];
  addJS(repoM, @"Fetch All", @"", 0, @"chithubMenu.fetchAll()");
  addJS(repoM, @"Pull All", @"", 0, @"chithubMenu.pullAll()");
  addJS(repoM, @"Push All", @"", 0, @"chithubMenu.pushAll()");
  [repoI setSubmenu:repoM];

  // Window menu
  NSMenuItem *winI = [[NSMenuItem alloc] init];
  [bar addItem:winI];
  NSMenu *winM = [[NSMenu alloc] initWithTitle:@"Window"];
  [winM addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
  [winM addItemWithTitle:@"Zoom" action:@selector(performZoom:) keyEquivalent:@""];
  [winI setSubmenu:winM];

  [NSApp setMainMenu:bar];
  [NSApp setWindowsMenu:winM];
}

// chitRun shows the UI in a native WKWebView window and runs the app loop. It
// returns only when the process is terminated (window closed / Quit).
static void chitRun(const char *curl, const char *ctitle,
                    const void *iconBytes, int iconLen, int w, int h) {
  @autoreleasepool {
    NSString *title = [NSString stringWithUTF8String:ctitle];
    NSApplication *app = [NSApplication sharedApplication];
    [app setActivationPolicy:NSApplicationActivationPolicyRegular];
    gChitDelegate = [[ChitDelegate alloc] init];
    [app setDelegate:gChitDelegate];

    if (iconBytes != NULL && iconLen > 0) {
      NSData *d = [NSData dataWithBytes:iconBytes length:iconLen];
      NSImage *img = [[NSImage alloc] initWithData:d];
      if (img) [app setApplicationIconImage:img];
    }

    NSRect frame = NSMakeRect(0, 0, w, h);
    NSWindow *win = [[NSWindow alloc] initWithContentRect:frame
      styleMask:(NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|
                 NSWindowStyleMaskMiniaturizable|NSWindowStyleMaskResizable)
      backing:NSBackingStoreBuffered defer:NO];
    [win setTitle:title];
    [win setReleasedWhenClosed:NO];
    [win setFrameAutosaveName:@"ChitHubMain"];
    [win center];

    WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
    WKWebView *web = [[WKWebView alloc] initWithFrame:frame configuration:cfg];
    gWebView = web; // menu items run JS against this
    NSURL *u = [NSURL URLWithString:[NSString stringWithUTF8String:curl]];
    [web loadRequest:[NSURLRequest requestWithURL:u]];
    [win setContentView:web];

    chitMenu(title); // build the menu now that gWebView is set

    [win makeKeyAndOrderFront:nil];
    [app activateIgnoringOtherApps:YES];
    [app run];
  }
}
*/
import "C"
import (
	_ "embed"
	"runtime"
	"unsafe"
)

//go:embed packaging/icon.icns
var appIconICNS []byte

func init() { runtime.LockOSThread() }

// appIcon returns the icon bytes used for the Dock (works even when run from a
// terminal, where there is no .app bundle to supply one).
func appIcon() []byte { return appIconICNS }

// nativeWindowSupported reports that this build can host a native window.
func nativeWindowSupported() bool { return true }

// runNativeWindow shows the UI in a native WKWebView window with its own Dock
// icon and menu, and blocks until the window is closed (which quits the app).
func runNativeWindow(url, title string, icon []byte, w, h int) {
	cu := C.CString(url)
	defer C.free(unsafe.Pointer(cu))
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	var ip unsafe.Pointer
	if len(icon) > 0 {
		ip = unsafe.Pointer(&icon[0])
	}
	C.chitRun(cu, ct, ip, C.int(len(icon)), C.int(w), C.int(h))
	runtime.KeepAlive(icon)
}
