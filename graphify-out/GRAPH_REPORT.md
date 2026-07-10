# Graph Report - .  (2026-07-09)

## Corpus Check
- Corpus is ~45,007 words - fits in a single context window. You may not need a graph.

## Summary
- 614 nodes · 1327 edges · 37 communities (33 shown, 4 thin omitted)
- Extraction: 74% EXTRACTED · 26% INFERRED · 0% AMBIGUOUS · INFERRED: 339 edges (avg confidence: 0.81)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Tag & Sum Cache|Tag & Sum Cache]]
- [[_COMMUNITY_Audio Decoding Pipeline|Audio Decoding Pipeline]]
- [[_COMMUNITY_Bluesky Integration|Bluesky Integration]]
- [[_COMMUNITY_Blossom Storage|Blossom Storage]]
- [[_COMMUNITY_Audio Playback & TUI|Audio Playback & TUI]]
- [[_COMMUNITY_Configuration System|Configuration System]]
- [[_COMMUNITY_Architecture & ADRs|Architecture & ADRs]]
- [[_COMMUNITY_Blossom Servers & Earmarks|Blossom Servers & Earmarks]]
- [[_COMMUNITY_MPRIS Service Core|MPRIS Service Core]]
- [[_COMMUNITY_Nostr Integration|Nostr Integration]]
- [[_COMMUNITY_Filter Expression Engine|Filter Expression Engine]]
- [[_COMMUNITY_Search Engine|Search Engine]]
- [[_COMMUNITY_Nostr List (Tests)|Nostr List (Tests)]]
- [[_COMMUNITY_CICD & Distribution|CI/CD & Distribution]]
- [[_COMMUNITY_MPRIS Control Messages|MPRIS Control Messages]]
- [[_COMMUNITY_Speaker Platform (macOS)|Speaker Platform (macOS)]]
- [[_COMMUNITY_Speaker Platform (Windows)|Speaker Platform (Windows)]]
- [[_COMMUNITY_Agent Instruction Docs|Agent Instruction Docs]]
- [[_COMMUNITY_Audio Architecture (Docs)|Audio Architecture (Docs)]]
- [[_COMMUNITY_MPRIS Stubs (macOS)|MPRIS Stubs (macOS)]]
- [[_COMMUNITY_MPRIS Stubs (Windows)|MPRIS Stubs (Windows)]]
- [[_COMMUNITY_Speaker Core|Speaker Core]]
- [[_COMMUNITY_Installer Script|Installer Script]]
- [[_COMMUNITY_ListenBrainz Scrobbling|ListenBrainz Scrobbling]]
- [[_COMMUNITY_Development Standards|Development Standards]]
- [[_COMMUNITY_Filter Matching|Filter Matching]]
- [[_COMMUNITY_Main Entry Point|Main Entry Point]]
- [[_COMMUNITY_Path Fixer Utility|Path Fixer Utility]]
- [[_COMMUNITY_Script Utilities|Script Utilities]]
- [[_COMMUNITY_Issue Tracking|Issue Tracking]]
- [[_COMMUNITY_Package Root|Package Root]]

## God Nodes (most connected - your core abstractions)
1. `AudioPlayer` - 28 edges
2. `contains()` - 27 edges
3. `PlayerModel` - 24 edges
4. `MPRISService` - 24 edges
5. `NewPlayerModel()` - 23 edges
6. `LoadConfig()` - 22 edges
7. `T` - 22 edges
8. `T` - 20 edges
9. `Model` - 20 edges
10. `NewSumCache()` - 19 edges

## Surprising Connections (you probably didn't know these)
- `Model (GEMINI.md ref)` --semantically_similar_to--> `Model (model.go)`  [INFERRED] [semantically similar]
  GEMINI.md → CLAUDE.md
- `AudioPlayer (GEMINI.md ref)` --semantically_similar_to--> `AudioPlayer (audio.go)`  [INFERRED] [semantically similar]
  GEMINI.md → CLAUDE.md
- `Speaker (GEMINI.md ref)` --semantically_similar_to--> `Speaker (speaker.go)`  [INFERRED] [semantically similar]
  GEMINI.md → CLAUDE.md
- `ListenBrainz (GEMINI.md ref)` --semantically_similar_to--> `ListenBrainz (listenbrainz.go)`  [INFERRED] [semantically similar]
  GEMINI.md → CLAUDE.md
- `Shuffle (GEMINI.md ref)` --semantically_similar_to--> `Shuffle (shuffle.go)`  [INFERRED] [semantically similar]
  GEMINI.md → CLAUDE.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Tag Index Design Space (ADR-0001 Alternatives)** — adr_0001_tags_external_index_external_tag_index, adr_0001_tags_external_index_alternative_infile, adr_0001_tags_external_index_alternative_chromaprint [EXTRACTED 1.00]
- **Release Pipeline (build -> distribute -> verify)** — workflows_release_goreleaser_release, workflows_apt_repo_apt_repository, workflows_chocolatey_publish_chocolatey_publish, workflows_smoke_tests_smoke_tests [INFERRED 0.95]
- **Dual Agent Instruction Docs (CLAUDE.md + GEMINI.md describe same architecture)** — claude_audioplayer, claude_speaker, claude_listenbrainz, claude_shuffle, claude_model, claude_main_go, claude_bubbletea, claude_beep, gemini_audioplayer, gemini_speaker, gemini_listenbrainz, gemini_shuffle, gemini_model, gemini_main_go, gemini_bubbletea, gemini_beep [EXTRACTED 1.00]

## Communities (37 total, 4 thin omitted)

### Community 0 - "Tag & Sum Cache"
Cohesion: 0.07
Nodes (54): SumCache, SumCacheEntry, TagIndex, FileMode, Context, SumCache, IndexSource(), copyFile() (+46 more)

### Community 1 - "Audio Decoding Pipeline"
Cohesion: 0.06
Nodes (28): Duration, SampleRate, Streamer, Time, NewAudioPlayer(), resampleForDevice(), drain(), Streamer (+20 more)

### Community 2 - "Bluesky Integration"
Cohesion: 0.11
Nodes (45): buildBskyPostText(), bytesIndex(), createBskySession(), Context, normalizeBlueskyHandle(), PublishBskyPost(), T, TestBuildBskyPostText_ArtistOnly() (+37 more)

### Community 3 - "Blossom Storage"
Cohesion: 0.13
Nodes (43): blossomAuthToken(), decryptChunk(), deleteChunk(), DeleteManifestChunks(), DownloadAndReassemble(), downloadChunk(), downloadChunkWithFallback(), encryptChunk() (+35 more)

### Community 4 - "Audio Playback & TUI"
Cohesion: 0.09
Nodes (26): AudioPlayer, Cmd, cleanupMsg, indexProgressMsg, nostrPublishedMsg, PlayerModel, playErrorMsg, positionMsg (+18 more)

### Community 5 - "Configuration System"
Cohesion: 0.14
Nodes (37): Command, configDir(), configFilePath(), LoadConfig(), LoadListenBrainzToken(), resolveBskyConfig(), SaveConfig(), Config (+29 more)

### Community 6 - "Architecture & ADRs"
Cohesion: 0.06
Nodes (40): Chromaprint/AcoustID Alternative (rejected), In-File Metadata Alternative (rejected), External Tag Index (ADR-0001), tag.Sum Audio Fingerprint, Domain Docs Consumption Pattern, dhowden/tag, Indexer (indexer.go), Shuffle (shuffle.go) (+32 more)

### Community 7 - "Blossom Servers & Earmarks"
Cohesion: 0.16
Nodes (32): LoadBlossomServers(), ResolveBlossomServers(), BlossomManifest, Earmark, Earmark, AppendToQueue(), FlushQueue(), LoadQueue() (+24 more)

### Community 8 - "MPRIS Service Core"
Cohesion: 0.13
Nodes (14): Conn, mprisStateSnapshot, Error, basenameWithoutExt(), MPRISService, Duration, PlayerModel, Program (+6 more)

### Community 9 - "Nostr Integration"
Cohesion: 0.16
Nodes (25): fetchBlossomServers(), LoadNostrRelays(), Event, Filter, fetchUserWriteRelays(), Context, npubFromPrivateKey(), PublishNostrTrackNote() (+17 more)

### Community 10 - "Filter Expression Engine"
Cohesion: 0.19
Nodes (14): andExpr, Expr, FilterExpr(), ParseExpr(), tokenize(), TestParseExpr(), TestParseExprErrors(), orExpr (+6 more)

### Community 11 - "Search Engine"
Cohesion: 0.24
Nodes (20): SearchLinks, buildSearchQuery(), fetchSearchPage(), FindTrackLinks(), searchBandcamp(), searchYouTube(), T, TestBandcampTrackRe() (+12 more)

### Community 12 - "Nostr List (Tests)"
Cohesion: 0.35
Nodes (11): selfConvKey(), T, TestCleanupPartitionsEarmarks(), TestEarmarkJSONRoundTrip(), TestEarmarkMaxAge(), TestEarmarkTimestamp(), TestEncryptDecryptRoundTrip(), TestEncryptDecryptWrongKey() (+3 more)

### Community 13 - "CI/CD & Distribution"
Cohesion: 0.24
Nodes (11): goreleaser Multi-Platform Build, Homebrew Formula (punkscience/homebrew-derpy), nfpm .deb Package, APT Installation (Debian/Ubuntu), Chocolatey Installation (Windows), Homebrew Installation (macOS), Chocolatey Verification Procedure, APT Repository Workflow (+3 more)

### Community 14 - "MPRIS Control Messages"
Cohesion: 0.22
Nodes (9): mprisNextMsg, mprisPauseMsg, mprisPlayMsg, mprisPlayPauseMsg, mprisPreviousMsg, mprisSeekMsg, mprisSetPositionMsg, mprisStopMsg (+1 more)

### Community 15 - "Speaker Platform (macOS)"
Cohesion: 0.20
Nodes (5): beepReader, SampleRate, Streamer, speakerInit(), speakerPlay()

### Community 16 - "Speaker Platform (Windows)"
Cohesion: 0.20
Nodes (5): beepReader, SampleRate, Streamer, speakerInit(), speakerPlay()

### Community 17 - "Agent Instruction Docs"
Cohesion: 0.22
Nodes (9): Bubble Tea (charmbracelet/bubbletea), Keyboard Controls, Model (model.go), 100ms TUI Tick Interval, Bubble Tea (GEMINI.md), File Deletion [D] (GEMINI.md), Keyboard Controls (GEMINI.md), Model (GEMINI.md ref) (+1 more)

### Community 18 - "Audio Architecture (Docs)"
Cohesion: 0.25
Nodes (8): AudioPlayer (audio.go), Beep (gopxl/beep), jfreymuth/pulse, Speaker (speaker.go), Audio Safety (speakerLock/Unlock), AudioPlayer (GEMINI.md ref), Beep (GEMINI.md), Speaker (GEMINI.md ref)

### Community 19 - "MPRIS Stubs (macOS)"
Cohesion: 0.32
Nodes (4): MPRISService, PlayerModel, Program, NewMPRISService()

### Community 20 - "MPRIS Stubs (Windows)"
Cohesion: 0.32
Nodes (4): MPRISService, PlayerModel, Program, NewMPRISService()

### Community 21 - "Speaker Core"
Cohesion: 0.25
Nodes (4): SampleRate, Streamer, speakerInit(), speakerPlay()

### Community 22 - "Installer Script"
Cohesion: 0.53
Nodes (4): install.sh script, install_linux(), install_macos(), install_windows()

### Community 23 - "ListenBrainz Scrobbling"
Cohesion: 0.40
Nodes (5): hirigaray/go-listenbrainz, ListenBrainz (listenbrainz.go), ListenBrainz Scrobbling, go-listenbrainz (GEMINI.md), ListenBrainz (GEMINI.md ref)

### Community 24 - "Development Standards"
Cohesion: 0.50
Nodes (4): Cobra CLI Library, Development Directives, SOLID Principles, Cobra CLI (GEMINI.md)

### Community 25 - "Filter Matching"
Cohesion: 0.83
Nodes (3): Filter(), lowerAll(), matches()

### Community 26 - "Main Entry Point"
Cohesion: 0.67
Nodes (3): Entry Point (main.go), Default Source Directory, Entry Point (GEMINI.md ref)

## Knowledge Gaps
- **87 isolated node(s):** `SampleRate`, `StreamSeekCloser`, `Ctrl`, `Format`, `File` (+82 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **4 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `NewPlayerModel()` connect `Bluesky Integration` to `Tag & Sum Cache`, `Audio Decoding Pipeline`, `Audio Playback & TUI`, `Configuration System`?**
  _High betweenness centrality (0.150) - this node is a cross-community bridge._
- **Why does `NewAudioPlayer()` connect `Audio Decoding Pipeline` to `Bluesky Integration`?**
  _High betweenness centrality (0.108) - this node is a cross-community bridge._
- **Why does `contains()` connect `Bluesky Integration` to `Tag & Sum Cache`, `Blossom Storage`, `Configuration System`, `Nostr Integration`, `Filter Expression Engine`, `Search Engine`, `Filter Matching`?**
  _High betweenness centrality (0.106) - this node is a cross-community bridge._
- **Are the 24 inferred relationships involving `contains()` (e.g. with `TestDownloadChunkSHA256Mismatch()` and `buildBskyPostText()`) actually correct?**
  _`contains()` has 24 INFERRED edges - model-reasoned connections that need verification._
- **Are the 21 inferred relationships involving `NewPlayerModel()` (e.g. with `TestViewHidesIndexingStatusWhenDone()` and `TestViewShowsIndexingStatusLine()`) actually correct?**
  _`NewPlayerModel()` has 21 INFERRED edges - model-reasoned connections that need verification._
- **What connects `SampleRate`, `StreamSeekCloser`, `Ctrl` to the rest of the system?**
  _93 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Tag & Sum Cache` be split into smaller, more focused modules?**
  _Cohesion score 0.06804214223002635 - nodes in this community are weakly interconnected._