#import <Cocoa/Cocoa.h>

static void dingConfigureGlass(NSView *view) {
    if ([view isKindOfClass:[NSVisualEffectView class]]) {
        NSVisualEffectView *glass = (NSVisualEffectView *)view;
        glass.material = NSVisualEffectMaterialSidebar;
        glass.blendingMode = NSVisualEffectBlendingModeBehindWindow;
        glass.state = NSVisualEffectStateActive;
        glass.emphasized = NO;
    }
    for (NSView *child in view.subviews) dingConfigureGlass(child);
}

void dingSetAppearance(int tone) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSAppearance *appearance = nil;
        if (tone == 1) appearance = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
        if (tone == 2) {
            if (@available(macOS 10.14, *)) appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
        }
        for (NSWindow *window in NSApp.windows) {
            window.appearance = appearance;
            dingConfigureGlass(window.contentView);
        }
    });
}
