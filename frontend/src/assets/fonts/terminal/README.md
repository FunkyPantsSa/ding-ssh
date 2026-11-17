# On-demand terminal fonts

Maple and Sarasa font binaries are downloaded on selection, not bundled. Cache Storage (ding-terminal-fonts-v1) persists successful downloads. If storage is unavailable or full, the browser HTTP cache is used. Cache eviction requires downloading again. Existing small Fontsource fonts remain bundled as offline fallbacks.

Maple Mono NF CN uses ZeoSeven FontsAPI item 442 (regular/medium/bold). All Unicode shards for a requested weight load before the terminal rebuilds its glyph atlas.

Sarasa Mono SC uses complete Regular/Bold WOFF2 files from xiaolai/claudepot-app commit 831df883498b1cecb9644ac1ba6c04c5d907e267 via jsDelivr. Regular is shared by 400/500.

See theme/remote-fonts.ts for URLs. Failed downloads can be retried by selecting the scheme again. Font licenses remain here and in public/licenses/fonts.
