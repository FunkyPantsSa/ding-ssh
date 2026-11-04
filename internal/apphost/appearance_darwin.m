#import <Cocoa/Cocoa.h>

void dingSetAppearance(int tone) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSAppearance *appearance = nil;
        if (tone == 1) appearance = [NSAppearance appearanceNamed:NSAppearanceNameAqua];
        if (tone == 2) appearance = [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
        for (NSWindow *window in NSApp.windows) {
            window.appearance = appearance;
        }
    });
}
