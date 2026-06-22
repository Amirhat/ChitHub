//go:build darwin

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

// A standard main menu. The App menu gives Cmd+Q; the Edit menu is what makes
// Cmd+C/V/X/A/Z work inside the web UI's text fields (e.g. the commit box).
static void chitMenu(NSString *name) {
  NSMenu *bar = [[NSMenu alloc] init];

  NSMenuItem *appI = [[NSMenuItem alloc] init];
  [bar addItem:appI];
  NSMenu *appM = [[NSMenu alloc] init];
  [appM addItemWithTitle:[@"About " stringByAppendingString:name]
                  action:@selector(orderFrontStandardAboutPanel:) keyEquivalent:@""];
  [appM addItem:[NSMenuItem separatorItem]];
  [appM addItemWithTitle:[@"Hide " stringByAppendingString:name] action:@selector(hide:) keyEquivalent:@"h"];
  NSMenuItem *others = [appM addItemWithTitle:@"Hide Others" action:@selector(hideOtherApplications:) keyEquivalent:@"h"];
  [others setKeyEquivalentModifierMask:(NSEventModifierFlagOption|NSEventModifierFlagCommand)];
  [appM addItem:[NSMenuItem separatorItem]];
  [appM addItemWithTitle:[@"Quit " stringByAppendingString:name] action:@selector(terminate:) keyEquivalent:@"q"];
  [appI setSubmenu:appM];

  NSMenuItem *editI = [[NSMenuItem alloc] init];
  [bar addItem:editI];
  NSMenu *editM = [[NSMenu alloc] initWithTitle:@"Edit"];
  [editM addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
  NSMenuItem *redo = [editM addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"z"];
  [redo setKeyEquivalentModifierMask:(NSEventModifierFlagShift|NSEventModifierFlagCommand)];
  [editM addItem:[NSMenuItem separatorItem]];
  [editM addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
  [editM addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
  [editM addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
  [editM addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
  [editI setSubmenu:editM];

  NSMenuItem *winI = [[NSMenuItem alloc] init];
  [bar addItem:winI];
  NSMenu *winM = [[NSMenu alloc] initWithTitle:@"Window"];
  [winM addItemWithTitle:@"Minimize" action:@selector(performMiniaturize:) keyEquivalent:@"m"];
  [winM addItemWithTitle:@"Close" action:@selector(performClose:) keyEquivalent:@"w"];
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

    chitMenu(title);

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
    NSURL *u = [NSURL URLWithString:[NSString stringWithUTF8String:curl]];
    [web loadRequest:[NSURLRequest requestWithURL:u]];
    [win setContentView:web];

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
