# Media sources and connector policy

Reviewed: 2026-08-29. Provider terms and APIs change; re-review before each production integration and at least quarterly thereafter.

## The practical answer

There is no legitimate API that means “all GIFs and movie clips.” The largest useful catalog is a federation of sources with different rules:

1. Licensed GIF/clip search providers for broad, current discovery.
2. Open or public-domain archives for a corpus GoGIF may transform and serve itself.
3. User uploads and GoGIF generations for the private/owned library.
4. Embed-only catalogs for discovery where downloading or transformation is prohibited.

Movie and television clips are usually copyrighted. Search availability does not grant a right to copy, edit, train on, or rehost an item. Rights and provenance are data, not paperwork to add later.

## Recommended source matrix

| Source | Role in GoGIF | Important constraints | Storage rule |
| --- | --- | --- | --- |
| [GIPHY GIF search](https://developers.giphy.com/docs/api/endpoint/search/) | Primary broad GIF/sticker discovery | Preserve ranking and attribution. GIPHY's [API terms](https://support.giphy.com/hc/en-us/articles/360028134111-GIPHY-API-Terms-of-Service) restrict combining its results with other providers without approval. | Store provider ID, query telemetry, and permitted cache data only; render media from returned URLs. |
| [GIPHY Clips](https://developers.giphy.com/docs/clips/) | Short clips with sound from official partners | Clip endpoints require prior approval. Use a separate source section and platform key. | Provider-hosted; never copied into the GoGIF catalog. |
| [KLIPY](https://docs.klipy.com/) | GIFs, stickers, memes, and clips; useful second provider | Its standard integration says to request media client-side, preserve ranking, display attribution, keep results separate, and not store/mirror/rehost without written approval. | Provider-hosted only under standard terms. |
| [Wikimedia Commons](https://commons.wikimedia.org/wiki/Commons:Simple_media_reuse_guide) | Best large seed corpus for reusable image/audio/video media | More than 146 million freely licensed/public-domain files were listed in June 2026, but the license and attribution are per file. Share-alike terms may follow derivatives. Fetch license metadata with [MediaWiki imageinfo](https://www.mediawiki.org/wiki/API:Imageinfo/en). | Provider-hosted. A selected, transformable item may be fetched temporarily; retain only provenance and the new output. |
| [Internet Archive moving images](https://archivesupport.zendesk.com/hc/en-us/articles/360017808151-Movies-and-Videos-A-Basic-Guide) | Huge archival video source, especially verified collections such as Prelinger | Internet Archive [does not guarantee rights](https://archivesupport.zendesk.com/hc/en-us/articles/360014759692-Rights); rights are item-specific. | Link to provider media. Allowlist collections and verify rights before any temporary transformation fetch. |
| [NASA Image and Video Library](https://images.nasa.gov/) | High-quality space/science media with rich metadata | NASA material is generally reusable in the US, but [usage rules](https://www.nasa.gov/nasa-brand-center/images-and-media/) still cover third-party material, logos, identifiable people, endorsement, and attribution. | Provider-hosted. Preserve credit and restrictions on newly generated derivatives. |
| [Pixabay API](https://pixabay.com/api/docs/) | User-directed stock-video search for creation inputs | API results must be cached for 24 hours and systematic mass downloads are prohibited. Follow the Pixabay Content License and attribution guidance. | Temporarily fetch only a user-selected creation input; never mirror the catalog. |
| [Pexels API](https://www.pexels.com/api/documentation/) | User-directed photo/video search for creation inputs | Attribution is required for API use. [API terms](https://help.pexels.com/hc/en-us/articles/900005880463-What-are-the-Terms-and-Conditions) prohibit systematic copying, core-product replication, redistribution, and AI dataset/training uses. | Use only for compliant user-directed workflows; do not mirror its catalog. |
| [YouTube Data API](https://developers.google.com/youtube/v3/docs/search/list) | Search and link/embed only | [Developer policies](https://developers.google.com/youtube/terms/developer-policies) prohibit downloading, caching audiovisual content, separating tracks, or modifying API content without approval. | Store video IDs and embed links only. Never use as a GIF-generation input source under standard API terms. |
| Yarn/GetYarn | Product inspiration and possible commercial partner | No public developer API or reusable-content license was located in the 2026-08-29 review. Do not scrape it. | No connector until Yarn grants explicit API access in writing. |

Tenor is excluded from a new integration because its API was decommissioned for new use in 2026. Keep its provider slot absent rather than building against a retired dependency.

## Search presentation

The single input can still feel universal while the results remain policy-safe:

```text
query
  ├── Created & yours       GoGIF catalog, freely rank and blend
  ├── Open media            verified Wikimedia/Archive/NASA assets
  ├── GIPHY                 provider-ranked, attributed section
  ├── KLIPY                 provider-ranked, attributed section
  └── Web references        link/embed-only results
```

Do not merge GIPHY and KLIPY items into one re-ranked feed under their standard terms. A sectioned result surface preserves the intuitive one-box experience without commingling providers.

## Provider adapter contract

Every adapter must implement the same normalized result shape while retaining provider policy:

- canonical provider and external IDs;
- source page and provider media URLs;
- title, tags, locale, content rating, author, and attribution;
- available renditions and dimensions;
- source handling mode: provider-hosted or temporary transformation reference;
- cache TTL and revalidation deadline;
- share/view analytics obligations;
- license and derivative/commercial permissions when known;
- raw provider rank, with no GoGIF re-ranking when prohibited.

Unknown rights are represented as `unknown`, never as permission.

## First provider sequence

1. Use the implemented Wikimedia Commons adapter for free, source-linked discovery.
2. Add controlled, temporary fetch-on-selection only for items whose recorded rights allow derivatives; discard source bytes after generation.
3. Add allowlisted Internet Archive/Prelinger and NASA adapters without mirroring their archives.
4. Keep GIPHY optional and sectioned when a platform key is explicitly configured.
5. Add Pixabay/Pexels only as user-directed editor inputs after a terms review.
6. Contact Yarn about an API/content partnership; do not scrape it or block the MVP on it.
