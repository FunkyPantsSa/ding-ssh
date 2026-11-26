//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdint.h>
#include <math.h>

extern int dingInputActive(uint64_t requestID, int start);
extern void dingInputComplete(uint64_t requestID, char *code, char *message);

static void inputFailure(uint64_t requestID, const char *code, NSString *message) {
    dingInputComplete(requestID, (char *)code, (char *)message.UTF8String);
}
static WKWebView *inputWebView(NSView *view) {
    if ([view isKindOfClass:[WKWebView class]]) return (WKWebView *)view;
    for(NSView *child in view.subviews) { WKWebView *found=inputWebView(child); if(found!=nil) return found; }
    return nil;
}
static NSEventModifierFlags inputModifiers(NSArray *mods) {
    NSEventModifierFlags flags=0;
    if(![mods isKindOfClass:[NSArray class]]) return flags;
    for(NSString *mod in mods) {
        if([mod isEqualToString:@"shift"]) flags|=NSEventModifierFlagShift;
        if([mod isEqualToString:@"ctrl"] || [mod isEqualToString:@"control"]) flags|=NSEventModifierFlagControl;
        if([mod isEqualToString:@"alt"] || [mod isEqualToString:@"option"]) flags|=NSEventModifierFlagOption;
        if([mod isEqualToString:@"meta"] || [mod isEqualToString:@"cmd"] || [mod isEqualToString:@"command"]) flags|=NSEventModifierFlagCommand;
    }
    return flags;
}
void dingInputWindow(uintptr_t handle,uint64_t requestID,const char *payload) {
    NSData *data=[NSData dataWithBytes:payload length:strlen(payload)];
    NSDictionary *args=[NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            if(!dingInputActive(requestID,0)) {inputFailure(requestID,"NATIVE_ACTION_CANCELLED",@"input cancelled before dispatch");return;}
            NSWindow *window=nil;
            for(NSWindow *candidate in NSApp.windows) if((uintptr_t)candidate==handle) {window=candidate;break;}
            if(window==nil) {inputFailure(requestID,"WINDOW_GONE",@"native window no longer belongs to this process");return;}
            if(!window.visible || window.miniaturized) {inputFailure(requestID,"WINDOW_NOT_VISIBLE",@"target window is hidden or minimized");return;}
            if(!window.keyWindow) {inputFailure(requestID,"WINDOW_NOT_FOCUSED",@"native input requires a focused target window; focus it explicitly first");return;}
            WKWebView *view=inputWebView(window.contentView);
            if(view==nil) {inputFailure(requestID,"WEBVIEW_UNAVAILABLE",@"no WKWebView found in the target window");return;}
            NSString *action=args[@"action"];
            NSEventModifierFlags mods=inputModifiers(args[@"modifiers"]);
            if([action isEqualToString:@"click"]) {
                if(window.ignoresMouseEvents) {inputFailure(requestID,"WINDOW_INPUT_DISABLED",@"target window ignores mouse events");return;}
                [window retain];[view retain];
                [view evaluateJavaScript:@"({width:window.innerWidth,height:window.innerHeight})" completionHandler:^(id viewport,NSError *error) {
                    @autoreleasepool {
                        if(error!=nil || ![viewport isKindOfClass:[NSDictionary class]]) {inputFailure(requestID,"VIEWPORT_UNAVAILABLE",@"cannot measure native input viewport");[window release];[view release];return;}
                        double x=[args[@"x"] doubleValue], y=[args[@"y"] doubleValue];
                        double width=[viewport[@"width"] doubleValue],height=[viewport[@"height"] doubleValue];
                        if(!isfinite(x)||!isfinite(y)||width<=0||height<=0||x<0||y<0||x>=width||y>=height) {inputFailure(requestID,"POINT_OUTSIDE_VIEWPORT",@"native click point is outside the WebView viewport");[window release];[view release];return;}
                        if(!window.keyWindow || window.miniaturized || !window.visible || !dingInputActive(requestID,1)) {inputFailure(requestID,"NATIVE_ACTION_CANCELLED",@"target focus or request changed before click dispatch");[window release];[view release];return;}
                        NSRect bounds=view.bounds;
                        double pointY=view.flipped ? y*bounds.size.height/height : bounds.size.height-y*bounds.size.height/height;
                        NSPoint point=[view convertPoint:NSMakePoint(x*bounds.size.width/width,pointY) toView:nil];
                        NSString *button=args[@"button"];
                        BOOL right=[button isEqualToString:@"right"];
                        NSEventType down=right?NSEventTypeRightMouseDown:NSEventTypeLeftMouseDown;
                        NSEventType up=right?NSEventTypeRightMouseUp:NSEventTypeLeftMouseUp;
                        NSTimeInterval stamp=NSProcessInfo.processInfo.systemUptime;
                        NSEvent *press=[NSEvent mouseEventWithType:down location:point modifierFlags:mods timestamp:stamp windowNumber:window.windowNumber context:nil eventNumber:0 clickCount:1 pressure:1.0];
                        NSEvent *release=[NSEvent mouseEventWithType:up location:point modifierFlags:mods timestamp:stamp+0.001 windowNumber:window.windowNumber context:nil eventNumber:1 clickCount:1 pressure:0.0];
                        [window sendEvent:press];
                        // WebKit forwards input to another process. Give the
                        // native run loop a turn between down/up rather than
                        // sending both in one synchronous dispatch.
                        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, 30*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
                            [window sendEvent:release];
                            dingInputComplete(requestID,NULL,NULL);[window release];[view release];
                        });
                    }
                }];
                return;
            }
            NSDictionary *special=@{@"Enter":@"\r",@"Tab":@"\t",@"Escape":@"\x1b",@"Backspace":@"\x7f",@"Delete":@"\uf728",@"ArrowUp":@"\uf700",@"ArrowDown":@"\uf701",@"ArrowLeft":@"\uf702",@"ArrowRight":@"\uf703",@"Home":@"\uf729",@"End":@"\uf72b",@"PageUp":@"\uf72c",@"PageDown":@"\uf72d",@"Space":@" "};
            NSDictionary *codes=@{@"Enter":@36,@"Tab":@48,@"Escape":@53,@"Backspace":@51,@"Delete":@117,@"ArrowUp":@126,@"ArrowDown":@125,@"ArrowLeft":@123,@"ArrowRight":@124,@"Home":@115,@"End":@119,@"PageUp":@116,@"PageDown":@121,@"Space":@49};
            id focusedResponder=window.firstResponder;
            if(![focusedResponder isKindOfClass:[NSView class]] || ![(NSView *)focusedResponder isDescendantOf:view]) {inputFailure(requestID,"WEBVIEW_NOT_FOCUSED",@"native key/text input requires focus within the target WebView");return;}
            NSString *key=args[@"key"] ?: @"";
            if([action isEqualToString:@"key"] && special[key]==nil && key.length!=1) {inputFailure(requestID,"NATIVE_KEY_UNSUPPORTED",@"key is not a supported native character or named key");return;}
            NSString *text=[action isEqualToString:@"type"]?args[@"text"]:(special[key] ?: key);
            if(text.length==0) {inputFailure(requestID,"EMPTY_NATIVE_INPUT",@"key or text is required");return;}
            if(!dingInputActive(requestID,1)) {inputFailure(requestID,"NATIVE_ACTION_CANCELLED",@"input cancelled before key dispatch");return;}
            if([action isEqualToString:@"type"]) {
                // insertText is the native text-input responder route. It does
                // not claim to simulate hardware keys, key chords or an IME.
                id responder=window.firstResponder;
                if(![responder respondsToSelector:@selector(insertText:replacementRange:)]) {inputFailure(requestID,"NATIVE_TEXT_UNSUPPORTED",@"focused native responder does not accept text input");return;}
                [(id<NSTextInputClient>)responder insertText:text replacementRange:NSMakeRange(NSNotFound,0)];
            } else {
                unsigned short code=codes[key]?[codes[key] unsignedShortValue]:0;
                NSTimeInterval stamp=NSProcessInfo.processInfo.systemUptime;
                NSEvent *down=[NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:mods timestamp:stamp windowNumber:window.windowNumber context:nil characters:text charactersIgnoringModifiers:text isARepeat:NO keyCode:code];
                NSEvent *up=[NSEvent keyEventWithType:NSEventTypeKeyUp location:NSZeroPoint modifierFlags:mods timestamp:stamp+0.001 windowNumber:window.windowNumber context:nil characters:text charactersIgnoringModifiers:text isARepeat:NO keyCode:code];
                [window sendEvent:down];[window sendEvent:up];
            }
            dingInputComplete(requestID,NULL,NULL);
        }
    });
}
