# Offline defaults and on-demand terminal fonts

The application ships one offline set: Geist Sans (UI, Latin 400/500/600), Geist Mono (terminal, Latin 400/500/600/700), and Noto Sans SC (complete Chinese coverage, 400). The default terminal scheme is `Geist Mono + Noto Sans SC` in both the frontend and Go settings. Existing saved choices are preserved.

`frontend/build/bundled-fonts.ts` generates the bundled CSS from the installed Fontsource packages, preserving every Noto Unicode range while selecting only WOFF2. Font files are never inlined so the resource budget accounts for all shipped font bytes. Every bundled Chinese shard loads before xterm rebuilds its glyph atlas, including characters outside the settings preview.

JetBrains Mono, Fira Code, Maple Mono NF CN and Sarasa Mono SC are downloaded only when selected. Cache Storage (`ding-terminal-fonts-v1`) persists successful downloads, with IndexedDB (`ding-remote-fonts`) as a fallback for native WebViews without Cache Storage. Unavailable/full storage does not prevent using a downloaded font. Cache eviction requires downloading again. Network failures retain the bundled fallback, and Settings provides a retry button. Existing terminals refresh when a retry succeeds. Partially downloaded Maple weights are not published to the document.

JetBrains Mono and Fira Code use pinned Fontsource 5.3.0 Latin WOFF2 files via jsDelivr, loading the requested normal weight and 700 for bold output.

Maple Mono NF CN uses ZeoSeven FontsAPI item 442 (regular/medium/bold). All Unicode shards for a requested weight load before the terminal rebuilds its glyph atlas.

Sarasa Mono SC uses complete Regular/Bold WOFF2 files from xiaolai/claudepot-app commit 831df883498b1cecb9644ac1ba6c04c5d907e267 via jsDelivr. Regular is shared by 400/500.

See `theme/font-catalog.ts` and `theme/remote-fonts.ts` for the font inventory and URLs. Bundled and downloadable font licenses ship in `public/licenses/fonts`.

`scripts/verify-ui2.sh` permits at most 3 MiB of bundled fonts, 128 font files/declarations (including Chinese Unicode shards), and 7 MiB of total frontend disk usage. Other JS limits and CSS warnings remain independent of the font allowance.
