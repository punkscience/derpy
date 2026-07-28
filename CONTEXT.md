# derpy

Terminal music player whose opinion is: shuffle everything, mark what's resonating, let the marks expire. This document is the glossary — implementation details live in code and ADRs.

## Language

**Track**:
A single audio file (mp3, flac, wav, ogg, m4a, aac) on disk that derpy can play.
_Avoid_: Song, file (when referring to playable content)

**Earmark**:
A user-flagged Track that "hit right" in the moment. Expires after 30 days. Encrypted, uploaded to Blossom, and recorded in a private Nostr list that follows the user's identity across devices.
_Avoid_: Favourite, bookmark, like

**Tag**:
A free-form, lowercase, user-authored string label attached to a Track for the purpose of later search/filter inside derpy. Permanent (no expiry). Stored in a derpy-local index keyed by the Track's Sum, never written into the audio file itself.
_Avoid_: Label, keyword, genre (Genre is a separate metadata field already present in the audio file's own headers)

**Sum**:
The metadata-invariant fingerprint of a Track's audio data, computed via `github.com/dhowden/tag`'s `Sum` function (SHA-1 of audio bytes, skipping ID3/MP4/FLAC metadata blocks per-format). Used as the primary key for the Tag index. Survives renames and embedded-metadata edits, but not re-encoding.
_Avoid_: Hash, fingerprint, checksum (when referring to derpy's specific tag-side identifier)

**Sum cache**:
The mtime+size-validated `path → Sum` mapping at `~/.config/derpy/sum-cache.json`. Bridges the file-system view (paths) and the Tag index (Sums). Populated lazily by the background indexer.

**Tag index**:
The `Sum → [Tag, ...]` mapping at `~/.config/derpy/tags.json`. Source of truth for what Tags a Track has.

## Relationships

- A **Track** has exactly one **Sum**
- A **Track** has zero or more **Tags**
- A **Track** is independently either earmarked or not — **Earmark** state and **Tag** set never influence each other
- **Tags** are derpy-local; **Earmarks** are user-identity-global (synced via Nostr)
- The **Sum cache** maps file-system paths to **Sums**; the **Tag index** maps **Sums** to **Tags**. The two together let derpy answer "what Tags does the file at this path have?" and "what paths have this Tag?"

## Flagged ambiguities

- "tag" was initially conflated with **ID3v2 frames** / **Vorbis Comments** (the audio file's own embedded metadata). Resolved: a derpy **Tag** is *not* written into the audio file; the audio file's embedded metadata is separately referred to as "embedded metadata" or by its format-specific name (ID3v2, Vorbis Comments).
- "m3u" was used to mean the file's embedded tag standard. Resolved: M3U is a playlist format and is unrelated to embedded metadata; it is not used by derpy and not in this glossary.
- "fingerprint" / "hash" was used ambiguously. Resolved: derpy uses **Sum** for the tag-side identifier. The Earmark feature's chunk SHA-256s (used for Blossom addressing) are a different hash and don't share the term.

## Example dialogue

> **User:** "I want to tag this **Track** as 'dnb' and later play all my dnb tracks."
> **Dev:** "So a **Tag** is a search affordance — file-local, no expiry, never travels off this machine. Different concept from an **Earmark**, which is about-the-moment and follows you across devices."
> **User:** "Right. And the **Tag** doesn't go inside the file — it's in a derpy-side index keyed by **Sum**."
> **Dev:** "Sum being metadata-invariant means if you rename the file or retag it with Picard, the **Tag** stays attached."

**Channel** — A named room of Nostr identities who share Earmarks with each
other. Membership is set by the channel's creator. Every message is NIP-59 gift
wrapped, so no relay can tell a channel exists. A channel post hands members the
file key for a copy already on Blossom; nothing is re-uploaded, and posts stay
playable for 30 days. There is no backfill: joining shows you only what is
posted afterwards. Defined in `docs/PROTOCOL.md` in the earmark repo.
