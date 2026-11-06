//go:build darwin

// Command adewindow is ade's window: an NSWindow holding a WKWebView,
// serving app/renderer over loopback. See .git/ade/docs/webview-host.md.
package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit

#include <stdlib.h>
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

// AdeScheme answers the webview scheme the panels are written against,
// by reading the same loopback server the window itself is served from.
@interface AdeScheme : NSObject <WKURLSchemeHandler>
@property(nonatomic, copy) NSString *base;
@end

@implementation AdeScheme
- (void)webView:(WKWebView *)view startURLSchemeTask:(id<WKURLSchemeTask>)task {
  NSURL *asked = task.request.URL;
  // ade-webview://asset/dist/x.js and ade-webview://panel/<id>, as the
  // host serves them: the authority is the first path element.
  NSString *path = [NSString stringWithFormat:@"%@/host/%@%@", self.base, asked.host,
                                              asked.path.length ? asked.path : @"/"];
  NSURLRequest *req = [NSURLRequest requestWithURL:[NSURL URLWithString:path]];
  [[NSURLSession.sharedSession
      dataTaskWithRequest:req
        completionHandler:^(NSData *data, NSURLResponse *res, NSError *err) {
          if (err != nil || data == nil) {
            [task didFailWithError:err ?: [NSError errorWithDomain:@"ade" code:1 userInfo:nil]];
            return;
          }
          NSHTTPURLResponse *http = (NSHTTPURLResponse *)res;
          NSURLResponse *said = [[NSURLResponse alloc]
               initWithURL:asked
                  MIMEType:http.MIMEType ?: @"application/octet-stream"
     expectedContentLength:(NSInteger)data.length
          textEncodingName:http.textEncodingName];
          [task didReceiveResponse:said];
          [task didReceiveData:data];
          [task didFinish];
        }] resume];
}
- (void)webView:(WKWebView *)view stopURLSchemeTask:(id<WKURLSchemeTask>)task {
}
@end

// lights puts the three window buttons where ade's own chrome expects
// them, which is also where Electron's trafficLightPosition had them.
static void lights(NSWindow *win) {
  NSButton *close = [win standardWindowButton:NSWindowCloseButton];
  NSButton *mini = [win standardWindowButton:NSWindowMiniaturizeButton];
  NSButton *zoom = [win standardWindowButton:NSWindowZoomButton];
  if (close == nil || close.superview == nil) return;
  CGFloat top = close.superview.bounds.size.height;
  CGFloat step = NSMinX(mini.frame) - NSMinX(close.frame);
  NSButton *all[3] = {close, mini, zoom};
  for (int i = 0; i < 3; i++) {
    NSRect at = all[i].frame;
    at.origin.x = 14 + step * i;
    at.origin.y = top - 12 - at.size.height;
    [all[i] setFrame:at];
  }
}

// AdeDelegate ends the process with the window: Cmd-W would otherwise leave
// an app running with nothing on screen and no way back to it.
@interface AdeDelegate : NSObject <NSApplicationDelegate>
@end

@implementation AdeDelegate
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)app {
  return YES;
}
@end

static NSMenuItem *item(NSMenu *menu, NSString *name, SEL act, NSString *key,
                        NSEventModifierFlags mods) {
  NSMenuItem *one = [menu addItemWithTitle:name action:act keyEquivalent:key];
  if (mods) one.keyEquivalentModifierMask = mods;
  return one;
}

// menus is why Cmd-C and Cmd-V work at all: AppKit routes them through the
// Edit menu's key equivalents, and a window without one just beeps.
static void menus(NSString *name) {
  NSMenu *bar = [[NSMenu alloc] init];

  NSMenuItem *appItem = [bar addItemWithTitle:@"" action:nil keyEquivalent:@""];
  NSMenu *app = [[NSMenu alloc] init];
  item(app, [@"Hide " stringByAppendingString:name], @selector(hide:), @"h", 0);
  item(app, @"Hide Others", @selector(hideOtherApplications:), @"h",
       NSEventModifierFlagCommand | NSEventModifierFlagOption);
  item(app, @"Show All", @selector(unhideAllApplications:), @"", 0);
  [app addItem:[NSMenuItem separatorItem]];
  item(app, [@"Quit " stringByAppendingString:name], @selector(terminate:), @"q", 0);
  appItem.submenu = app;

  NSMenuItem *editItem = [bar addItemWithTitle:@"Edit" action:nil keyEquivalent:@""];
  NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
  item(edit, @"Undo", @selector(undo:), @"z", 0);
  item(edit, @"Redo", @selector(redo:), @"z",
       NSEventModifierFlagCommand | NSEventModifierFlagShift);
  [edit addItem:[NSMenuItem separatorItem]];
  item(edit, @"Cut", @selector(cut:), @"x", 0);
  item(edit, @"Copy", @selector(copy:), @"c", 0);
  item(edit, @"Paste", @selector(paste:), @"v", 0);
  item(edit, @"Select All", @selector(selectAll:), @"a", 0);
  editItem.submenu = edit;

  NSMenuItem *winItem = [bar addItemWithTitle:@"Window" action:nil keyEquivalent:@""];
  NSMenu *window = [[NSMenu alloc] initWithTitle:@"Window"];
  item(window, @"Minimize", @selector(performMiniaturize:), @"m", 0);
  item(window, @"Zoom", @selector(performZoom:), @"", 0);
  item(window, @"Close", @selector(performClose:), @"w", 0);
  winItem.submenu = window;

  NSApp.mainMenu = bar;
  NSApp.windowsMenu = window;
}

static void RunWindow(const char *url, const char *title) {
  @autoreleasepool {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    menus([NSString stringWithUTF8String:title]);
    // Held: NSApp.delegate is weak, and ARC frees an unheld one at once.
    static AdeDelegate *kept;
    kept = [[AdeDelegate alloc] init];
    NSApp.delegate = kept;

    NSRect frame = NSMakeRect(0, 0, 1440, 900);
    NSWindow *win = [[NSWindow alloc]
        initWithContentRect:frame
                  styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                             NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable |
                             NSWindowStyleMaskFullSizeContentView)
                    backing:NSBackingStoreBuffered
                      defer:NO];
    [win setTitle:[NSString stringWithUTF8String:title]];
    // ade draws its own titlebar: the omnibox sits where one would be.
    win.titlebarAppearsTransparent = YES;
    win.titleVisibility = NSWindowTitleHidden;
    win.backgroundColor = [NSColor colorWithSRGBRed:0.051 green:0.063 blue:0.090 alpha:1.0];
    [win setMinSize:NSMakeSize(900, 600)];

    NSString *base = [NSString stringWithUTF8String:url];
    AdeScheme *scheme = [[AdeScheme alloc] init];
    scheme.base = base;
    WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
    [cfg setURLSchemeHandler:scheme forURLScheme:@"ade-webview"];

    WKWebView *web = [[WKWebView alloc] initWithFrame:frame configuration:cfg];
    [web setAutoresizingMask:(NSViewWidthSizable | NSViewHeightSizable)];
    web.allowsMagnification = NO;
    [win setContentView:web];

    [web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:base]]];

    lights(win);
    [NSNotificationCenter.defaultCenter addObserverForName:NSWindowDidResizeNotification
                                                    object:win
                                                     queue:NSOperationQueue.mainQueue
                                                usingBlock:^(NSNotification *note) {
                                                  lights(win);
                                                }];

    [win center];
    [win makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
    [NSApp run];
  }
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// held is the panels' pages, kept until the window asks for one by url.
type held struct {
	mu   sync.Mutex
	html map[string]string
}

func main() {
	runtime.LockOSThread()

	// Before anything is started: ade, the launcher and every session server
	// inherit this, and the agent's own PTY execs claude off it.
	if path := shellPath(); path != "" {
		_ = os.Setenv("PATH", path)
		slog.Debug("took the login shell's PATH", "entries", strings.Count(path, ":")+1)
	}

	root, err := assets()
	if err != nil {
		log.Fatalf("find the window's files: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen on loopback: %v", err)
	}
	self := fmt.Sprintf("http://%s", listener.Addr().String())

	mux := http.NewServeMux()
	mount(mux, root, &held{html: map[string]string{}}, opened())

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("ade's window on %s", self)
	if os.Getenv("ADE_WINDOW_SERVE") == "1" {
		log.Fatal(server.Serve(listener))
	}
	go func() {
		// A panic here would take the window with it, and the page would
		// see only its sockets drop.
		defer func() { _ = recover() }()
		log.Println(server.Serve(listener))
	}()

	where := C.CString(self + "/")
	title := C.CString("ade")
	defer C.free(unsafe.Pointer(where))
	defer C.free(unsafe.Pointer(title))
	C.RunWindow(where, title)
}

// mount is every route the page needs. The launcher is proxied rather
// than reached across origins, so the token never leaves this process.
func mount(mux *http.ServeMux, root string, panels *held, on string) {
	renderer := filepath.Join(root, "renderer")

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.FileServer(http.Dir(renderer)).ServeHTTP(w, r)
			return
		}
		page, err := shell(filepath.Join(renderer, "index.html"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	// The panels' own assets, which they ask for over the webview scheme.
	for _, dir := range []string{"dist", "media"} {
		at := http.FileServer(http.Dir(filepath.Join(root, dir)))
		mux.Handle("GET /"+dir+"/", http.StripPrefix("/"+dir+"/", at))
		mux.Handle("GET /host/asset/"+dir+"/", http.StripPrefix("/host/asset/"+dir+"/", at))
	}

	mux.Handle("/ade/", http.StripPrefix("/ade", &tokened{}))

	// The page opens the terminal socket itself, so it needs the one
	// address that reaches a session.
	mux.HandleFunc("GET /host/where", func(w http.ResponseWriter, _ *http.Request) {
		base, token, err := launcher()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			writeJSON(w, map[string]string{"error": "ade is not running."})
			return
		}
		writeJSON(w, map[string]string{"base": base, "token": token})
	})
	mux.HandleFunc("GET /host/opened", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"dir": on})
	})
	mux.HandleFunc("GET /host/keys", func(w http.ResponseWriter, _ *http.Request) {
		raw, err := os.ReadFile(filepath.Join(home(), ".ade", "keys.json")) // #nosec G304 -- the user's own
		if err != nil {
			writeJSON(w, []any{})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	// The window asks for a browser; the host is the only thing that can
	// open one, and `ade start` the only thing that brings the launcher up.
	mux.HandleFunc("POST /host/open", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Href string `json:"href"`
		}
		if json.NewDecoder(r.Body).Decode(&req) == nil && strings.HasPrefix(req.Href, "http") {
			_ = exec.Command("open", req.Href).Start() // #nosec G204 -- checked prefix
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /host/start", func(w http.ResponseWriter, _ *http.Request) {
		at, err := adeBinary()
		if err == nil {
			err = exec.Command(at, "start").Start() // #nosec G204 -- resolved, not given
		}
		if err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("PUT /host/panel/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, err := readAll(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		panels.put(r.PathValue("id"), body)
		writeJSON(w, map[string]string{"url": "ade-webview://panel/" + r.PathValue("id")})
	})
	mux.HandleFunc("GET /host/panel/{id}", func(w http.ResponseWriter, r *http.Request) {
		page, ok := panels.get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
}

// tokened is how the page reaches ade. The record is read per request, so
// a launcher started later, or moved, needs no new window.
type tokened struct {
	mu    sync.Mutex
	base  string
	token string
	proxy *httputil.ReverseProxy
}

// at is the proxy for whatever the record says now, rebuilt when it moves.
func (t *tokened) at() (*httputil.ReverseProxy, string, error) {
	base, token, err := launcher()
	if err != nil {
		return nil, "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.proxy == nil || base != t.base || token != t.token {
		to, err := url.Parse(base)
		if err != nil {
			return nil, "", err
		}
		proxy := httputil.NewSingleHostReverseProxy(to)
		// An event stream is only an event stream if it is not buffered.
		proxy.FlushInterval = -1
		t.base, t.token, t.proxy = base, token, proxy
	}
	return t.proxy, t.token, nil
}

func (t *tokened) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	proxy, token, err := t.at()
	if err != nil {
		slog.Debug("no launcher to reach", "err", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "ade is not running. Start it from a checkout."})
		return
	}
	r.Header.Set("X-Ade-Token", token)
	proxy.ServeHTTP(w, r)
}

func (h *held) put(id, html string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.html[id] = html
}

func (h *held) get(id string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	page, ok := h.html[id]
	return page, ok
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
