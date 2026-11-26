//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdint.h>

extern int dingCaptureActive(uint64_t requestID);
extern void dingCaptureComplete(uint64_t requestID, void *bytes, size_t length, int width, int height,
    double viewportWidth, double viewportHeight, double contentX, double contentY,
    double contentWidth, double contentHeight, char *errorCode, char *errorMessage);

static void captureFailure(uint64_t requestID, const char *code, NSString *message) {
    dingCaptureComplete(requestID, NULL, 0, 0, 0, 0, 0, 0, 0, 0, 0, (char *)code, (char *)message.UTF8String);
}

// Search the public NSView hierarchy. This is deliberately a version probe,
// rather than accessing Wails private fields or assuming a fixed view index.
static WKWebView *findWebView(NSView *view) {
    if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *found = findWebView(child);
        if (found != nil) return found;
    }
    return nil;
}

void dingCaptureWindow(uintptr_t windowHandle, uint64_t requestID) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            if (!dingCaptureActive(requestID)) { captureFailure(requestID, "CAPTURE_CANCELLED", @"capture was cancelled before native dispatch"); return; }
            NSWindow *window = nil;
            // Do not dereference a possibly stale native pointer. Resolve it
            // against the live application's windows on the UI thread.
            for (NSWindow *candidate in NSApp.windows) {
                if ((uintptr_t)candidate == windowHandle) { window = candidate; break; }
            }
            if (window == nil) { captureFailure(requestID, "WINDOW_GONE", @"native window no longer belongs to this application"); return; }
            if (window.miniaturized || !window.visible) { captureFailure(requestID, "WINDOW_NOT_VISIBLE", @"capture requires a visible, non-minimized window"); return; }
            WKWebView *view = findWebView(window.contentView);
            if (view == nil) { captureFailure(requestID, "WEBVIEW_UNAVAILABLE", @"no WKWebView found in the public native view hierarchy"); return; }
            NSRect bounds = view.bounds;
            CGFloat scale = window.backingScaleFactor;
            if (bounds.size.width <= 0 || bounds.size.height <= 0 || bounds.size.width * bounds.size.height * scale * scale > 32.0 * 1024 * 1024) {
                captureFailure(requestID, "CAPTURE_SIZE_LIMIT", @"WebView bitmap exceeds the capture pixel budget"); return;
            }
            [view retain];
            // Read actual CSS viewport dimensions; multiplying DPR, page zoom
            // and application scale would count some transforms twice.
            [view evaluateJavaScript:@"({width:window.innerWidth,height:window.innerHeight})" completionHandler:^(id value, NSError *evalError) {
                @autoreleasepool {
                    if (!dingCaptureActive(requestID)) { captureFailure(requestID, "CAPTURE_CANCELLED", @"capture was cancelled while measuring the viewport"); [view release]; return; }
                    if (evalError != nil || ![value isKindOfClass:[NSDictionary class]]) {
                        captureFailure(requestID, "VIEWPORT_UNAVAILABLE", evalError.localizedDescription ?: @"viewport measurement failed"); [view release]; return;
                    }
                    double viewportWidth = [value[@"width"] doubleValue], viewportHeight = [value[@"height"] doubleValue];
                    if (viewportWidth <= 0 || viewportHeight <= 0) { captureFailure(requestID, "VIEWPORT_UNAVAILABLE", @"viewport has no drawable area"); [view release]; return; }
                    NSRect contentRect = [view convertRect:view.bounds toView:window.contentView];
                    // Coordinates are Cocoa content-view points. The Go frame
                    // separately maps viewport CSS pixels to bitmap pixels.
                    [view takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image, NSError *error) {
                        @autoreleasepool {
                            if (!dingCaptureActive(requestID)) { captureFailure(requestID, "CAPTURE_CANCELLED", @"capture was cancelled while WebKit rendered the image"); [view release]; return; }
                            if (error != nil || image == nil) { captureFailure(requestID, "CAPTURE_FAILED", error.localizedDescription ?: @"WebKit did not return a snapshot"); [view release]; return; }
                            CGImageRef cgImage = [image CGImageForProposedRect:NULL context:nil hints:nil];
                            if (cgImage == NULL) { captureFailure(requestID, "CAPTURE_FAILED", @"WebKit snapshot has no bitmap representation"); [view release]; return; }
                            size_t width = CGImageGetWidth(cgImage), height = CGImageGetHeight(cgImage);
                            if (width == 0 || height == 0 || width > 16384 || height > 16384 || width * height > 32 * 1024 * 1024) {
                                captureFailure(requestID, "CAPTURE_SIZE_LIMIT", @"snapshot exceeds the capture pixel budget"); [view release]; return;
                            }
                            NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc] initWithCGImage:cgImage];
                            NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                            if (png == nil || png.length > 32 * 1024 * 1024) { captureFailure(requestID, "CAPTURE_SIZE_LIMIT", @"snapshot PNG is unavailable or exceeds the encoding budget"); }
                            else { dingCaptureComplete(requestID, (void *)png.bytes, png.length, (int)width, (int)height,
                                viewportWidth, viewportHeight, contentRect.origin.x, contentRect.origin.y, contentRect.size.width, contentRect.size.height, NULL, NULL); }
                            [bitmap release];
                            [view release];
                        }
                    }];
                }
            }];
        }
    });
}
